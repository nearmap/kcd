package ecr

import (
	"testing"
)

func TestVersionRegex(t *testing.T) {
	registryProvider, err := NewECR("951896542015.dkr.ecr.us-west-1.amazonaws.com/contextlogic/robbie", VersionRegex, nil)
	if err != nil {
		t.Errorf("err should not be nil to initialize the ECR Provider for unit test")
	}
	if !registryProvider.vRegex.MatchString("d21e94a3") {
		t.Errorf("d21e94a3 shoud be matched")
	}
	if !registryProvider.vRegex.MatchString("10293f4") {
		t.Errorf("10293f4 shoud be matched")
	}
	if !registryProvider.vRegex.MatchString("10293f4149c0baf908e1e5608ecf228b7ff2e165") {
		t.Errorf("10293f4149c0baf908e1e5608ecf228b7ff2e165 shoud be matched")
	}
	if registryProvider.vRegex.MatchString("master1671212064") {
		t.Errorf("master1671212064 shoud not be matched")
	}
	if registryProvider.vRegex.MatchString("my_awesome_branch") {
		t.Errorf("my_awesome_branch shoud not be matched")
	}
	if registryProvider.vRegex.MatchString("master") {
		t.Errorf("master shoud not be matched")
	}
	if registryProvider.vRegex.MatchString("prod") {
		t.Errorf("prod shoud not be matched")
	}
	if registryProvider.vRegex.MatchString("staging") {
		t.Errorf("staging shoud not be matched")
	}
	if registryProvider.vRegex.MatchString("dev") {
		t.Errorf("dev shoud not be matched")
	}

}
