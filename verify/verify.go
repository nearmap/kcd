package verify

type Verifier interface {
	Verify() error
}

type BasicVerifier struct {
	containerImage string
}

// NewBasicVerifier runs a verification action by initializing a pod
// with the given container immage and checking its exit code.
// The Verify action passes if the image has a zero exit code.
func NewBasicVerifier(containerImage string) *BasicVerifier {
	return &BasicVerifier{
		containerImage: containerImage,
	}
}

func (bv *BasicVerifier) Verify() error {
	// TODO:
	return nil
}
