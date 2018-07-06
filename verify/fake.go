package verify

// FakeVerifier is a fake Verifier implementation for use in unit tests.
type FakeVerifier struct {
	// Err is the error the Verify method will return.
	Err error
}

// NewFake returns a FakeVerifier instance for use in unit tests.
func NewFake() *FakeVerifier {
	return &FakeVerifier{}
}

// Verify implements the Verifier interface.
func (fv *FakeVerifier) Verify() error {
	return fv.Err
}
