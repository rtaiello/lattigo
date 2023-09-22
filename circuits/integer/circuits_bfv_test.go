package integer

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/big"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tuneinsight/lattigo/v4/bfv"
	"github.com/tuneinsight/lattigo/v4/bgv"
	"github.com/tuneinsight/lattigo/v4/ring"
	"github.com/tuneinsight/lattigo/v4/rlwe"
	"github.com/tuneinsight/lattigo/v4/utils"
	"github.com/tuneinsight/lattigo/v4/utils/bignum"
	"github.com/tuneinsight/lattigo/v4/utils/sampling"
)

var flagPrintNoise = flag.Bool("print-noise", false, "print the residual noise")
var flagParamString = flag.String("params", "", "specify the test cryptographic parameters as a JSON string. Overrides -short.")

func GetTestName(opname string, p bgv.Parameters, lvl int) string {
	return fmt.Sprintf("%s/LogN=%d/logQ=%d/logP=%d/logT=%d/Qi=%d/Pi=%d/lvl=%d",
		opname,
		p.LogN(),
		int(math.Round(p.LogQ())),
		int(math.Round(p.LogP())),
		int(math.Round(p.LogT())),
		p.QCount(),
		p.PCount(),
		lvl)
}

var (

	// These parameters are for test purpose only and are not 128-bit secure.
	testInsecure = bgv.ParametersLiteral{
		LogN: 10,
		Q:    []uint64{0x3fffffa8001, 0x1000090001, 0x10000c8001, 0x10000f0001, 0xffff00001},
		P:    []uint64{0x7fffffd8001},
	}

	testPlaintextModulus = []uint64{0x101, 0xffc001}

	testParams = []bgv.ParametersLiteral{testInsecure}
)

func TestBFV(t *testing.T) {

	var err error

	paramsLiterals := testParams

	if *flagParamString != "" {
		var jsonParams bgv.ParametersLiteral
		if err = json.Unmarshal([]byte(*flagParamString), &jsonParams); err != nil {
			t.Fatal(err)
		}
		paramsLiterals = []bgv.ParametersLiteral{jsonParams} // the custom test suite reads the parameters from the -params flag
	}

	for _, p := range paramsLiterals[:] {

		for _, plaintextModulus := range testPlaintextModulus[:] {

			p.PlaintextModulus = plaintextModulus

			params, err := bfv.NewParametersFromLiteral(bfv.ParametersLiteral(p))
			require.NoError(t, err)

			tc, err := genTestParams(params)
			require.NoError(t, err)

			for _, testSet := range []func(tc *testContext, t *testing.T){
				testLinearTransformation,
			} {
				testSet(tc, t)
				runtime.GC()
			}
		}
	}
}

func testLinearTransformation(tc *testContext, t *testing.T) {

	level := tc.params.MaxLevel()
	t.Run(GetTestName("Evaluator/LinearTransform/BSGS=true", bgv.Parameters(tc.params.Parameters), level), func(t *testing.T) {

		params := tc.params

		values, _, ciphertext := newBFVTestVectorsLvl(level, tc.params.DefaultScale(), tc, tc.encryptorSk)

		diagonals := make(Diagonals[uint64])

		totSlots := values.N()

		diagonals[-15] = make([]uint64, totSlots)
		diagonals[-4] = make([]uint64, totSlots)
		diagonals[-1] = make([]uint64, totSlots)
		diagonals[0] = make([]uint64, totSlots)
		diagonals[1] = make([]uint64, totSlots)
		diagonals[2] = make([]uint64, totSlots)
		diagonals[3] = make([]uint64, totSlots)
		diagonals[4] = make([]uint64, totSlots)
		diagonals[15] = make([]uint64, totSlots)

		for i := 0; i < totSlots; i++ {
			diagonals[-15][i] = 1
			diagonals[-4][i] = 1
			diagonals[-1][i] = 1
			diagonals[0][i] = 1
			diagonals[1][i] = 1
			diagonals[2][i] = 1
			diagonals[3][i] = 1
			diagonals[4][i] = 1
			diagonals[15][i] = 1
		}

		ltparams := LinearTransformationParameters{
			DiagonalsIndexList:       []int{-15, -4, -1, 0, 1, 2, 3, 4, 15},
			Level:                    ciphertext.Level(),
			Scale:                    tc.params.DefaultScale(),
			LogDimensions:            ciphertext.LogDimensions,
			LogBabyStepGianStepRatio: 1,
		}

		// Allocate the linear transformation
		linTransf := NewLinearTransformation(params, ltparams)

		// Encode on the linear transformation
		require.NoError(t, EncodeLinearTransformation[uint64](tc.encoder, diagonals, linTransf))

		galEls := GaloisElementsForLinearTransformation(params, ltparams)

		ltEval := NewLinearTransformationEvaluator(tc.evaluator.WithKey(rlwe.NewMemEvaluationKeySet(nil, tc.kgen.GenGaloisKeysNew(galEls, tc.sk)...)))

		require.NoError(t, ltEval.Evaluate(ciphertext, linTransf, ciphertext))

		tmp := make([]uint64, totSlots)
		copy(tmp, values.Coeffs[0])

		subRing := tc.params.RingT().SubRings[0]

		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, -15), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, -4), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, -1), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 1), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 2), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 3), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 4), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 15), values.Coeffs[0])

		verifyBFVTestVectors(tc, tc.decryptor, values, ciphertext, t)
	})

	t.Run(GetTestName("Evaluator/LinearTransform/BSGS=false", bgv.Parameters(tc.params.Parameters), level), func(t *testing.T) {

		params := tc.params

		values, _, ciphertext := newBFVTestVectorsLvl(level, tc.params.DefaultScale(), tc, tc.encryptorSk)

		diagonals := make(Diagonals[uint64])

		totSlots := values.N()

		diagonals[-15] = make([]uint64, totSlots)
		diagonals[-4] = make([]uint64, totSlots)
		diagonals[-1] = make([]uint64, totSlots)
		diagonals[0] = make([]uint64, totSlots)
		diagonals[1] = make([]uint64, totSlots)
		diagonals[2] = make([]uint64, totSlots)
		diagonals[3] = make([]uint64, totSlots)
		diagonals[4] = make([]uint64, totSlots)
		diagonals[15] = make([]uint64, totSlots)

		for i := 0; i < totSlots; i++ {
			diagonals[-15][i] = 1
			diagonals[-4][i] = 1
			diagonals[-1][i] = 1
			diagonals[0][i] = 1
			diagonals[1][i] = 1
			diagonals[2][i] = 1
			diagonals[3][i] = 1
			diagonals[4][i] = 1
			diagonals[15][i] = 1
		}

		ltparams := LinearTransformationParameters{
			DiagonalsIndexList:       []int{-15, -4, -1, 0, 1, 2, 3, 4, 15},
			Level:                    ciphertext.Level(),
			Scale:                    tc.params.DefaultScale(),
			LogDimensions:            ciphertext.LogDimensions,
			LogBabyStepGianStepRatio: -1,
		}

		// Allocate the linear transformation
		linTransf := NewLinearTransformation(params, ltparams)

		// Encode on the linear transformation
		require.NoError(t, EncodeLinearTransformation[uint64](tc.encoder, diagonals, linTransf))

		galEls := GaloisElementsForLinearTransformation(params, ltparams)

		ltEval := NewLinearTransformationEvaluator(tc.evaluator.WithKey(rlwe.NewMemEvaluationKeySet(nil, tc.kgen.GenGaloisKeysNew(galEls, tc.sk)...)))

		require.NoError(t, ltEval.Evaluate(ciphertext, linTransf, ciphertext))

		tmp := make([]uint64, totSlots)
		copy(tmp, values.Coeffs[0])

		subRing := tc.params.RingT().SubRings[0]

		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, -15), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, -4), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, -1), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 1), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 2), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 3), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 4), values.Coeffs[0])
		subRing.Add(values.Coeffs[0], utils.RotateSlotsNew(tmp, 15), values.Coeffs[0])

		verifyBFVTestVectors(tc, tc.decryptor, values, ciphertext, t)
	})

	t.Run("PolyEval", func(t *testing.T) {

		polyEval := NewPolynomialEvaluator(tc.params.Parameters, tc.evaluator.Evaluator, true)

		t.Run("Single", func(t *testing.T) {

			if tc.params.MaxLevel() < 4 {
				t.Skip("MaxLevel() to low")
			}

			values, _, ciphertext := newBFVTestVectorsLvl(tc.params.MaxLevel(), tc.params.NewScale(1), tc, tc.encryptorSk)

			coeffs := []uint64{1, 2, 3, 4, 5, 6, 7, 8}

			T := tc.params.PlaintextModulus()
			for i := range values.Coeffs[0] {
				values.Coeffs[0][i] = ring.EvalPolyModP(values.Coeffs[0][i], coeffs, T)
			}

			poly := bignum.NewPolynomial(bignum.Monomial, coeffs, nil)

			res, err := polyEval.Evaluate(ciphertext, poly, tc.params.DefaultScale()) // TODO simpler interface for BFV ?
			require.NoError(t, err)

			require.True(t, res.Scale.Cmp(tc.params.DefaultScale()) == 0)

			verifyBFVTestVectors(tc, tc.decryptor, values, res, t)

		})

		t.Run("Vector", func(t *testing.T) {

			if tc.params.MaxLevel() < 4 {
				t.Skip("MaxLevel() to low")
			}

			values, _, ciphertext := newBFVTestVectorsLvl(tc.params.MaxLevel(), tc.params.NewScale(7), tc, tc.encryptorSk)

			coeffs0 := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
			coeffs1 := []uint64{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}

			slots := values.N()

			slotIndex := make(map[int][]int)
			idx0 := make([]int, slots>>1)
			idx1 := make([]int, slots>>1)
			for i := 0; i < slots>>1; i++ {
				idx0[i] = 2 * i
				idx1[i] = 2*i + 1
			}

			slotIndex[0] = idx0
			slotIndex[1] = idx1

			polyVector, err := NewPolynomialVector([][]uint64{
				coeffs0,
				coeffs1,
			}, slotIndex)
			require.NoError(t, err)

			TInt := new(big.Int).SetUint64(tc.params.PlaintextModulus())
			for pol, idx := range slotIndex {
				for _, i := range idx {
					values.Coeffs[0][i] = polyVector.Value[pol].EvaluateModP(new(big.Int).SetUint64(values.Coeffs[0][i]), TInt).Uint64()
				}
			}

			res, err := polyEval.Evaluate(ciphertext, polyVector, tc.params.DefaultScale())
			require.NoError(t, err)

			require.True(t, res.Scale.Cmp(tc.params.DefaultScale()) == 0)

			verifyBFVTestVectors(tc, tc.decryptor, values, res, t)

		})
	})
}

type testContext struct {
	params      bfv.Parameters
	ringQ       *ring.Ring
	ringT       *ring.Ring
	prng        sampling.PRNG
	uSampler    *ring.UniformSampler
	encoder     *bgv.Encoder
	kgen        *rlwe.KeyGenerator
	sk          *rlwe.SecretKey
	pk          *rlwe.PublicKey
	encryptorPk *rlwe.Encryptor
	encryptorSk *rlwe.Encryptor
	decryptor   *rlwe.Decryptor
	evaluator   *bfv.Evaluator
	testLevel   []int
}

func genTestParams(params bfv.Parameters) (tc *testContext, err error) {

	tc = new(testContext)
	tc.params = params

	if tc.prng, err = sampling.NewPRNG(); err != nil {
		return nil, err
	}

	tc.ringQ = params.RingQ()
	tc.ringT = params.RingT()

	tc.uSampler = ring.NewUniformSampler(tc.prng, tc.ringT)
	tc.kgen = bfv.NewKeyGenerator(tc.params)
	tc.sk, tc.pk = tc.kgen.GenKeyPairNew()
	tc.encoder = bgv.NewEncoder(bgv.Parameters(tc.params.Parameters))

	tc.encryptorPk = bfv.NewEncryptor(tc.params, tc.pk)
	tc.encryptorSk = bfv.NewEncryptor(tc.params, tc.sk)
	tc.decryptor = bfv.NewDecryptor(tc.params, tc.sk)
	tc.evaluator = bfv.NewEvaluator(tc.params, rlwe.NewMemEvaluationKeySet(tc.kgen.GenRelinearizationKeyNew(tc.sk)))

	tc.testLevel = []int{0, params.MaxLevel()}

	return
}

func newBFVTestVectorsLvl(level int, scale rlwe.Scale, tc *testContext, encryptor *rlwe.Encryptor) (coeffs ring.Poly, plaintext *rlwe.Plaintext, ciphertext *rlwe.Ciphertext) {
	coeffs = tc.uSampler.ReadNew()
	for i := range coeffs.Coeffs[0] {
		coeffs.Coeffs[0][i] = uint64(i)
	}
	plaintext = bfv.NewPlaintext(tc.params, level)
	plaintext.Scale = scale
	tc.encoder.Encode(coeffs.Coeffs[0], plaintext)
	if encryptor != nil {
		var err error
		ciphertext, err = encryptor.EncryptNew(plaintext)
		if err != nil {
			panic(err)
		}
	}

	return coeffs, plaintext, ciphertext
}

func verifyBFVTestVectors(tc *testContext, decryptor *rlwe.Decryptor, coeffs ring.Poly, element rlwe.ElementInterface[ring.Poly], t *testing.T) {

	coeffsTest := make([]uint64, tc.params.MaxSlots())

	switch el := element.(type) {
	case *rlwe.Plaintext:
		require.NoError(t, tc.encoder.Decode(el, coeffsTest))
	case *rlwe.Ciphertext:

		pt := decryptor.DecryptNew(el)

		require.NoError(t, tc.encoder.Decode(pt, coeffsTest))

		if *flagPrintNoise {
			require.NoError(t, tc.encoder.Encode(coeffsTest, pt))
			ct, err := tc.evaluator.SubNew(el, pt)
			require.NoError(t, err)
			vartmp, _, _ := rlwe.Norm(ct, decryptor)
			t.Logf("STD(noise): %f\n", vartmp)
		}

	default:
		t.Fatal("invalid test object to verify")
	}

	require.True(t, utils.EqualSlice(coeffs.Coeffs[0], coeffsTest))
}
