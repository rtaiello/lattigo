package rlwe

import (
	"math"
	"math/big"

	"github.com/tuneinsight/lattigo/v4/ring"
	"github.com/tuneinsight/lattigo/v4/rlwe/ringqp"
	"github.com/tuneinsight/lattigo/v4/utils/bignum"
)

// PublicKeyIsCorrect returns true if pk is a correct RLWE public-key for secret-key sk and parameters params.
func PublicKeyIsCorrect(pk *PublicKey, sk *SecretKey, params Parameters, log2Bound float64) bool {

	pk = pk.CopyNew()

	levelQ, levelP := params.MaxLevelQ(), params.MaxLevelP()
	ringQP := params.RingQP().AtLevel(levelQ, levelP)

	// [-as + e] + [as]
	ringQP.MulCoeffsMontgomeryThenAdd(&sk.Value, pk.Value[1], pk.Value[0])
	ringQP.INTT(pk.Value[0], pk.Value[0])
	ringQP.IMForm(pk.Value[0], pk.Value[0])

	if log2Bound <= ringQP.RingQ.Log2OfStandardDeviation(pk.Value[0].Q) {
		return false
	}

	if ringQP.RingP != nil && log2Bound <= ringQP.RingP.Log2OfStandardDeviation(pk.Value[0].P) {
		return false
	}

	return true
}

// RelinearizationKeyIsCorrect returns true if evk is a correct RLWE relinearization-key for secret-key sk and parameters params.
func RelinearizationKeyIsCorrect(rlk *RelinearizationKey, sk *SecretKey, params Parameters, log2Bound float64) bool {
	levelQ, levelP := params.MaxLevelQ(), params.MaxLevelP()
	sk2 := sk.CopyNew()
	params.RingQP().AtLevel(levelQ, levelP).MulCoeffsMontgomery(&sk2.Value, &sk2.Value, &sk2.Value)
	return EvaluationKeyIsCorrect(rlk.EvaluationKey.CopyNew(), sk2, sk, params, log2Bound)
}

// GaloisKeyIsCorrect returns true if evk is a correct EvaluationKey for galois element galEl, secret-key sk and parameters params.
func GaloisKeyIsCorrect(gk *GaloisKey, sk *SecretKey, params Parameters, log2Bound float64) bool {

	skIn := sk.CopyNew()
	skOut := sk.CopyNew()

	nthRoot := params.RingQ().NthRoot()

	galElInv := ring.ModExp(gk.GaloisElement, nthRoot-1, nthRoot)

	ringQ, ringP := params.RingQ(), params.RingP()

	ringQ.AutomorphismNTT(sk.Value.Q, galElInv, skOut.Value.Q)
	if ringP != nil {
		ringP.AutomorphismNTT(sk.Value.P, galElInv, skOut.Value.P)
	}

	return EvaluationKeyIsCorrect(&gk.EvaluationKey, skIn, skOut, params, log2Bound)
}

// EvaluationKeyIsCorrect returns true if evk is a correct EvaluationKey for input key skIn, output key skOut and parameters params.
func EvaluationKeyIsCorrect(evk *EvaluationKey, skIn, skOut *SecretKey, params Parameters, log2Bound float64) bool {
	evk = evk.CopyNew()
	skIn = skIn.CopyNew()
	levelQ, levelP := params.MaxLevelQ(), params.MaxLevelP()
	ringQP := params.RingQP().AtLevel(levelQ, levelP)
	ringQ, ringP := ringQP.RingQ, ringQP.RingP
	decompPw2 := params.DecompPw2(levelQ, levelP)

	// Decrypts
	// [-asIn + w*P*sOut + e, a] + [asIn]
	for i := range evk.Value {
		for j := range evk.Value[i] {
			ringQP.MulCoeffsMontgomeryThenAdd(evk.Value[i][j].Value[1], &skOut.Value, evk.Value[i][j].Value[0])
		}
	}

	// Sums all bases together (equivalent to multiplying with CRT decomposition of 1)
	// sum([1]_w * [RNS*PW2*P*sOut + e]) = PWw*P*sOut + sum(e)
	for i := range evk.Value { // RNS decomp
		if i > 0 {
			for j := range evk.Value[i] { // PW2 decomp
				ringQP.Add(evk.Value[0][j].Value[0], evk.Value[i][j].Value[0], evk.Value[0][j].Value[0])
			}
		}
	}

	if levelP != -1 {
		// sOut * P
		ringQ.MulScalarBigint(skIn.Value.Q, ringP.Modulus(), skIn.Value.Q)
	}

	for i := 0; i < decompPw2; i++ {

		// P*s^i + sum(e) - P*s^i = sum(e)
		ringQ.Sub(evk.Value[0][i].Value[0].Q, skIn.Value.Q, evk.Value[0][i].Value[0].Q)

		// Checks that the error is below the bound
		// Worst error bound is N * floor(6*sigma) * #Keys
		ringQP.INTT(evk.Value[0][i].Value[0], evk.Value[0][i].Value[0])
		ringQP.IMForm(evk.Value[0][i].Value[0], evk.Value[0][i].Value[0])

		// Worst bound of inner sum
		// N*#Keys*(N * #Parties * floor(sigma*6) + #Parties * floor(sigma*6) + N * #Parties  +  #Parties * floor(6*sigma))

		if log2Bound < ringQ.Log2OfStandardDeviation(evk.Value[0][i].Value[0].Q) {
			return false
		}

		if levelP != -1 {
			if log2Bound < ringP.Log2OfStandardDeviation(evk.Value[0][i].Value[0].P) {
				return false
			}
		}

		// sOut * P * PW2
		ringQ.MulScalar(skIn.Value.Q, 1<<params.Pow2Base(), skIn.Value.Q)
	}

	return true
}

// Norm returns the log2 of the standard deviation, minimum and maximum absolute norm of
// the decrypted Ciphertext, before the decoding (i.e. including the error).
func Norm(ct *Ciphertext, dec Decryptor) (std, min, max float64) {

	params := dec.(*decryptor).params

	coeffsBigint := make([]*big.Int, params.N())
	for i := range coeffsBigint {
		coeffsBigint[i] = new(big.Int)
	}

	pt := NewPlaintext(params, ct.Level())

	dec.Decrypt(ct, pt)

	ringQ := params.RingQ().AtLevel(ct.Level())

	if pt.IsNTT {
		ringQ.INTT(pt.Value, pt.Value)
	}

	ringQ.PolyToBigintCentered(pt.Value, 1, coeffsBigint)

	return NormStats(coeffsBigint)
}

func NormStats(vec []*big.Int) (float64, float64, float64) {

	vecfloat := make([]*big.Float, len(vec))
	minErr := new(big.Float).SetFloat64(0)
	maxErr := new(big.Float).SetFloat64(0)
	tmp := new(big.Float)
	minErr.SetInt(vec[0])
	minErr.Abs(minErr)
	for i := range vec {
		vecfloat[i] = new(big.Float)
		vecfloat[i].SetInt(vec[i])

		tmp.Abs(vecfloat[i])

		if minErr.Cmp(tmp) == 1 {
			minErr.Set(tmp)
		}

		if maxErr.Cmp(tmp) == -1 {
			maxErr.Set(tmp)
		}
	}

	n := new(big.Float).SetFloat64(float64(len(vec)))

	mean := new(big.Float).SetFloat64(0)

	for _, c := range vecfloat {
		mean.Add(mean, c)
	}

	mean.Quo(mean, n)

	err := new(big.Float).SetFloat64(0)
	for _, c := range vecfloat {
		tmp.Sub(c, mean)
		tmp.Mul(tmp, tmp)
		err.Add(err, tmp)
	}

	err.Quo(err, n)
	err.Sqrt(err)

	x, _ := err.Float64()
	y, _ := minErr.Float64()
	z, _ := maxErr.Float64()

	return math.Log2(x), math.Log2(y), math.Log2(z)
}

// FindBestBSGSRatio finds the best N1*N2 = N for the baby-step giant-step algorithm for matrix multiplication.
func FindBestBSGSRatio(diagMatrix interface{}, maxN int, logMaxRatio int) (minN int) {

	maxRatio := float64(int(1 << logMaxRatio))

	for N1 := 1; N1 < maxN; N1 <<= 1 {

		_, rotN1, rotN2 := BSGSIndex(diagMatrix, maxN, N1)

		nbN1, nbN2 := len(rotN1)-1, len(rotN2)-1

		if float64(nbN2)/float64(nbN1) == maxRatio {
			return N1
		}

		if float64(nbN2)/float64(nbN1) > maxRatio {
			return N1 / 2
		}
	}

	return 1
}

// BSGSIndex returns the index map and needed rotation for the BSGS matrix-vector multiplication algorithm.
func BSGSIndex(el interface{}, slots, N1 int) (index map[int][]int, rotN1, rotN2 []int) {
	index = make(map[int][]int)
	rotN1Map := make(map[int]bool)
	rotN2Map := make(map[int]bool)
	var nonZeroDiags []int
	switch element := el.(type) {
	case map[int][]complex128:
		nonZeroDiags = make([]int, len(element))
		var i int
		for key := range element {
			nonZeroDiags[i] = key
			i++
		}
	case map[int][]float64:
		nonZeroDiags = make([]int, len(element))
		var i int
		for key := range element {
			nonZeroDiags[i] = key
			i++
		}
	case map[int]bool:
		nonZeroDiags = make([]int, len(element))
		var i int
		for key := range element {
			nonZeroDiags[i] = key
			i++
		}
	case map[int]ringqp.Poly:
		nonZeroDiags = make([]int, len(element))
		var i int
		for key := range element {
			nonZeroDiags[i] = key
			i++
		}
	case map[int][]*big.Float:
		nonZeroDiags = make([]int, len(element))
		var i int
		for key := range element {
			nonZeroDiags[i] = key
			i++
		}
	case map[int][]*bignum.Complex:
		nonZeroDiags = make([]int, len(element))
		var i int
		for key := range element {
			nonZeroDiags[i] = key
			i++
		}
	case []int:
		nonZeroDiags = element
	}

	for _, rot := range nonZeroDiags {
		rot &= (slots - 1)
		idxN1 := ((rot / N1) * N1) & (slots - 1)
		idxN2 := rot & (N1 - 1)
		if index[idxN1] == nil {
			index[idxN1] = []int{idxN2}
		} else {
			index[idxN1] = append(index[idxN1], idxN2)
		}
		rotN1Map[idxN1] = true
		rotN2Map[idxN2] = true
	}

	rotN1 = []int{}
	for i := range rotN1Map {
		rotN1 = append(rotN1, i)
	}

	rotN2 = []int{}
	for i := range rotN2Map {
		rotN2 = append(rotN2, i)
	}

	return
}
