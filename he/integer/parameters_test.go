package integer_test

import (
	"github.com/tuneinsight/lattigo/v4/he/integer"
)

var (

	// testInsecure are insecure parameters used for the sole purpose of fast testing.
	testInsecure = integer.ParametersLiteral{
		LogN: 10,
		Q:    []uint64{0x3fffffa8001, 0x1000090001, 0x10000c8001, 0x10000f0001, 0xffff00001},
		P:    []uint64{0x7fffffd8001},
	}

	testPlaintextModulus = []uint64{0x101, 0xffc001}

	testParams = []integer.ParametersLiteral{testInsecure}
)
