package multiparty

import (
	"fmt"
	"io"

	"github.com/tuneinsight/lattigo/v6/core/rlwe"
	"github.com/tuneinsight/lattigo/v6/ring"
	"github.com/tuneinsight/lattigo/v6/ring/ringqp"
	"github.com/tuneinsight/lattigo/v6/utils/sampling"
	"github.com/tuneinsight/lattigo/v6/utils/structs"
)

// Thresholdizer is a type for generating secret-shares of [ringqp.Poly] types such that
// the resulting sharing has a t-out-of-N-threshold access-structure. It implements the
// "Thresholdize" operation as presented in "An Efficient Threshold Access-Structure
// for RLWE-Based Multiparty Homomorphic Encryption" (2022) by Mouchet, C., Bertrand, E.,
// and Hubaux, J. P. (https://eprint.iacr.org/2022/780).
//
// See the [multiparty] package README.md.
type Thresholdizer struct {
	params     *rlwe.Parameters
	ringQ      *ring.Ring
	ringQP     *ringqp.Ring
	usamplerQ  *ring.UniformSampler
	usamplerQP ringqp.UniformSampler
}

// Combiner is a type for generating t-out-of-t additive shares from local t-out-of-N
// shares. It implements the "Combine" operation as presented in "An Efficient Threshold
// Access-Structure for RLWE-Based Multiparty Homomorphic Encryption" (2022) by Mouchet, C.,
// Bertrand, E., and Hubaux, J. P. (https://eprint.iacr.org/2022/780).
type Combiner struct {
	ringQ          *ring.Ring
	ringQP         *ringqp.Ring
	threshold      int
	tmp1, tmp2     []uint64
	one            ring.RNSScalar
	lagrangeCoeffs map[ShamirPublicPoint]ring.RNSScalar
}

// ShamirPublicPoint is a type for Shamir public point associated with a party identity within
// the t-out-of-N-threshold scheme.
//
// See [Thresholdizer] and [Combiner] types.
type ShamirPublicPoint uint64

// ShamirPolynomialQP represents a polynomial with [ringqp.Poly] coefficients. It is used by the
// Thresholdizer type to produce t-out-of-N-threshold shares of an [ringqp.Poly].
//
// See [Thresholdizer] type.
type ShamirPolynomialQ struct {
	Value structs.Vector[ring.Poly]
}
type ShamirPolynomialQP struct {
	Value structs.Vector[ringqp.Poly]
}

// ShamirSecretShare represents a t-out-of-N-threshold secret-share.
//
// See [Thresholdizer] and [Combiner] types.
type ShamirSecretShareQ struct {
	ring.Poly
}

type ShamirSecretShareQP struct {
	ringqp.Poly
}

// NewThresholdizer creates a new [Thresholdizer] instance from parameters.
func NewThresholdizer(params rlwe.ParameterProvider) Thresholdizer {

	thr := Thresholdizer{}
	thr.params = params.GetRLWEParameters()
	thr.ringQ = thr.params.RingQ()
	thr.ringQP = thr.params.RingQP()

	prng, err := sampling.NewPRNG()

	// Sanity check, this error should not happen.
	if err != nil {
		panic(fmt.Errorf("could not initialize PRNG: %s", err))
	}
	thr.usamplerQ = ring.NewUniformSampler(prng, thr.ringQ)
	thr.usamplerQP = ringqp.NewUniformSampler(prng, *thr.params.RingQP())

	return thr
}
func (thr Thresholdizer) GenShamirPolynomialQ(threshold int, secret *SmudgeError) (ShamirPolynomialQ, error) {
	if threshold < 1 {
		return ShamirPolynomialQ{}, fmt.Errorf("threshold should be >= 1")
	}
	gen := make([]ring.Poly, int(threshold))
	gen[0] = *secret.Value.CopyNew()
	for i := 1; i < threshold; i++ {
		gen[i] = thr.ringQ.NewPoly()
		thr.usamplerQ.Read(gen[i])
	}

	return ShamirPolynomialQ{Value: structs.Vector[ring.Poly](gen)}, nil
}

// GenShamirPolynomialQP generates a new secret [ShamirPolynomial] to be used in the [Thresholdizer.GenShamirSecretShareQP] method.
// It does so by sampling a random polynomial of degree threshold - 1 and with its constant term equal to secret.
func (thr Thresholdizer) GenShamirPolynomialQP(threshold int, secret *rlwe.SecretKey) (ShamirPolynomialQP, error) {
	if threshold < 1 {
		return ShamirPolynomialQP{}, fmt.Errorf("threshold should be >= 1")
	}
	gen := make([]ringqp.Poly, int(threshold))
	gen[0] = *secret.Value.CopyNew()
	for i := 1; i < threshold; i++ {
		gen[i] = thr.ringQP.NewPoly()
		thr.usamplerQP.Read(gen[i])
	}

	return ShamirPolynomialQP{Value: structs.Vector[ringqp.Poly](gen)}, nil
}

// AllocateThresholdSecretShare allocates a [ShamirSecretShareQP] struct.
func (thr Thresholdizer) AllocateThresholdSecretShareQ() ShamirSecretShareQ {
	return ShamirSecretShareQ{thr.ringQ.NewPoly()}
}
func (thr Thresholdizer) AllocateThresholdSecretShare() ShamirSecretShareQP {
	return ShamirSecretShareQP{thr.ringQP.NewPoly()}
}

func (thr Thresholdizer) GenShamirSecretShareQ(recipient ShamirPublicPoint, secretPoly ShamirPolynomialQ, shareOut *ShamirSecretShareQ) {
	thr.ringQ.EvalPolyScalar(secretPoly.Value, uint64(recipient), shareOut.Poly)
}

// GenShamirSecretShareQP generates a secret share for the given recipient, identified by its [ShamirPublicPoint].
// The result is stored in ShareOut and should be sent to this party.
func (thr Thresholdizer) GenShamirSecretShareQP(recipient ShamirPublicPoint, secretPoly ShamirPolynomialQP, shareOut *ShamirSecretShareQP) {
	thr.ringQP.EvalPolyScalar(secretPoly.Value, uint64(recipient), shareOut.Poly)
}

// AggregateShares aggregates two [ShamirSecretShareQP] and stores the result in outShare.
func (thr Thresholdizer) AggregateShares(share1, share2 ShamirSecretShareQP, outShare *ShamirSecretShareQP) (err error) {
	if share1.LevelQ() != share2.LevelQ() || share1.LevelQ() != outShare.LevelQ() || share1.LevelP() != share2.LevelP() || share1.LevelP() != outShare.LevelP() {
		return fmt.Errorf("cannot AggregateShares: shares level do not match")
	}
	thr.ringQP.AtLevel(share1.LevelQ(), share1.LevelP()).Add(share1.Poly, share2.Poly, outShare.Poly)
	return
}

// NewCombiner creates a new [Combiner] struct from the parameters and the set of [ShamirPublicPoints]. Note that the other
// parameter may contain the instantiator's own [ShamirPublicPoint].
func NewCombiner(params rlwe.Parameters, own ShamirPublicPoint, others []ShamirPublicPoint, threshold int) Combiner {
	cmb := Combiner{}
	cmb.ringQ = params.RingQ()
	cmb.ringQP = params.RingQP()
	cmb.threshold = threshold
	cmb.tmp1, cmb.tmp2 = cmb.ringQP.NewRNSScalar(), cmb.ringQP.NewRNSScalar()
	cmb.one = cmb.ringQP.NewRNSScalarFromUInt64(1)

	qlen := cmb.ringQP.RingQ.ModuliChainLength()
	for i, s := range cmb.ringQP.RingQ.SubRings {
		cmb.one[i] = ring.MForm(cmb.one[i], s.Modulus, s.BRedConstant)
	}
	if cmb.ringQP.RingP != nil {
		for i, s := range cmb.ringQP.RingP.SubRings {
			cmb.one[i+qlen] = ring.MForm(cmb.one[i+qlen], s.Modulus, s.BRedConstant)
		}
	}

	// precomputes lagrange coefficient factors
	cmb.lagrangeCoeffs = make(map[ShamirPublicPoint]ring.RNSScalar)
	for _, spk := range others {
		if spk != own {
			cmb.lagrangeCoeffs[spk] = cmb.ringQP.NewRNSScalar()
			cmb.lagrangeCoeff(own, spk, cmb.lagrangeCoeffs[spk])
		}
	}

	return cmb
}
func (cmb Combiner) GenAdditiveShareQ(activesPoints []ShamirPublicPoint, ownPoint ShamirPublicPoint, ownShare KeySwitchShare, skOut *KeySwitchShare) (err error) {

	if len(activesPoints) < cmb.threshold {
		return fmt.Errorf("cannot GenAdditiveShare: Not enough active players to combine threshold shares")
	}

	prod := cmb.tmp2
	copy(prod, cmb.one)

	for _, active := range activesPoints[:cmb.threshold] {
		//Lagrange Interpolation with the public threshold key of other active players
		if active != ownPoint {
			cmb.tmp1 = cmb.lagrangeCoeffs[active]
			cmb.ringQ.MulRNSScalar(prod, cmb.tmp1, prod)
		}
	}

	cmb.ringQ.MulRNSScalarMontgomery(ownShare.Value, prod, skOut.Value)
	return
}

// GenAdditiveShareQP generates a t-out-of-t additive share of the secret from a local aggregated share ownSecret and the set of active identities, identified
// by their [ShamirPublicPoint]. It stores the resulting additive share in skOut.
func (cmb Combiner) GenAdditiveShareQP(activesPoints []ShamirPublicPoint, ownPoint ShamirPublicPoint, ownShare ShamirSecretShareQP, skOut *rlwe.SecretKey) (err error) {

	if len(activesPoints) < cmb.threshold {
		return fmt.Errorf("cannot GenAdditiveShare: Not enough active players to combine threshold shares")
	}

	prod := cmb.tmp2
	copy(prod, cmb.one)

	for _, active := range activesPoints[:cmb.threshold] {
		//Lagrange Interpolation with the public threshold key of other active players
		if active != ownPoint {
			cmb.tmp1 = cmb.lagrangeCoeffs[active]
			cmb.ringQP.MulRNSScalar(prod, cmb.tmp1, prod)
		}
	}

	cmb.ringQP.MulRNSScalarMontgomery(ownShare.Poly, prod, skOut.Value)
	return
}

func (cmb Combiner) lagrangeCoeff(thisKey ShamirPublicPoint, thatKey ShamirPublicPoint, lagCoeff []uint64) {

	this := cmb.ringQP.NewRNSScalarFromUInt64(uint64(thisKey))
	that := cmb.ringQP.NewRNSScalarFromUInt64(uint64(thatKey))

	cmb.ringQP.SubRNSScalar(that, this, lagCoeff)

	cmb.ringQP.Inverse(lagCoeff)

	cmb.ringQP.MulRNSScalar(lagCoeff, that, lagCoeff)
}

// BinarySize returns the serialized size of the object in bytes.
func (s ShamirSecretShareQP) BinarySize() int {
	return s.Poly.BinarySize()
}

// WriteTo writes the object on an [io.Writer]. It implements the [io.WriterTo]
// interface, and will write exactly object.BinarySize() bytes on w.
//
// Unless w implements the [buffer.Writer] interface (see lattigo/utils/buffer/writer.go),
// it will be wrapped into a [bufio.Writer]. Since this requires allocations, it
// is preferable to pass a [buffer.Writer] directly:
//
//   - When writing multiple times to a [io.Writer], it is preferable to first wrap the
//     [io.Writer] in a pre-allocated [bufio.Writer].
//   - When writing to a pre-allocated var b []byte, it is preferable to pass
//     buffer.NewBuffer(b) as w (see lattigo/utils/buffer/buffer.go).
func (s ShamirSecretShareQP) WriteTo(w io.Writer) (n int64, err error) {
	return s.Poly.WriteTo(w)
}

// ReadFrom reads on the object from an [io.Writer]. It implements the
// [io.ReaderFrom] interface.
//
// Unless r implements the [buffer.Reader] interface (see see lattigo/utils/buffer/reader.go),
// it will be wrapped into a [bufio.Reader]. Since this requires allocation, it
// is preferable to pass a [buffer.Reader] directly:
//
//   - When reading multiple values from a [io.Reader], it is preferable to first
//     first wrap [io.Reader] in a pre-allocated [bufio.Reader].
//   - When reading from a var b []byte, it is preferable to pass a buffer.NewBuffer(b)
//     as w (see lattigo/utils/buffer/buffer.go).
func (s *ShamirSecretShareQP) ReadFrom(r io.Reader) (n int64, err error) {
	return s.Poly.ReadFrom(r)
}

// MarshalBinary encodes the object into a binary form on a newly allocated slice of bytes.
func (s ShamirSecretShareQP) MarshalBinary() (p []byte, err error) {
	return s.Poly.MarshalBinary()
}

// UnmarshalBinary decodes a slice of bytes generated by
// [ShamirSecretShareQP.MarshalBinary] or [ShamirSecretShareQP.WriteTo] on the object.
func (s *ShamirSecretShareQP) UnmarshalBinary(p []byte) (err error) {
	return s.Poly.UnmarshalBinary(p)
}
