package lib

import (
	"testing"
)

func TestIni(t *testing.T) {
	var (
		ExpectedSections = []string{"auth", "booleanvalues", "cloudfiles"}
	)
	// read fixtures file
	conf, err := ParseIniFile("../fixtures/test.ini")
	if err != nil {
		t.Error(err)
	}

	// test GetSectionNames()
	if !equalSlices(conf.GetSectionNames(), ExpectedSections) {
		t.Errorf("Expected %v sections but found %v", ExpectedSections, conf.GetSectionNames())
	}

	// test values
	if conf.Get("CloudFiles", "[c]ontai[ner") != "ev-public" {
		t.Error("Failed fetching ev-public")
	}
	if conf.Get("Auth", "key") != "3331a2d651d746e8777333777f447827" {
		t.Error("Failed fetching auth key")
	}
	if conf.Get("Boolean Values", "alive") != "true" {
		t.Error("Failed fetching 'alive' value")
	}
	if conf.Get("Boolean Values", "dead") != "false" {
		t.Error("Failed fetching 'dead' value")
	}
	if conf.Get("cloud files", "account name") != "huge account ." {
		t.Error("Failed fetching [Cloud Files]/'account name' value")
	}
}

// TestConf() tests parsing of .conf files. They're similar to ini-files, except
// they do not have sections
func TestConf(t *testing.T) {
	var (
		ExpectedSections = []string{""}
	)

	conf, err := ParseIniFile("../fixtures/test.conf")
	if err != nil {
		t.Error(err)
	}

	// test GetSectionNames()
	if !equalSlices(conf.GetSectionNames(), ExpectedSections) {
		t.Errorf("Expected %v sections but found %v", ExpectedSections, conf.GetSectionNames())
	}

	if conf.Get("", "Country") != "USA" {
		t.Error("Failed fetching country")
	}
	if conf.Get("", "ZIP") != "78704" {
		t.Error("Failed fetching ZIP")
	}
	if conf.Get("", " city") != "Austin" {
		t.Error("Failed fetching city with leading space")
	}
	if conf.Get("", "Country") != "USA" {
		t.Error("Failed fetching country")
	}
	if conf.Get("", "street") != "Barton Springs Road" {
		t.Error("Failed fetching street")
	}
}
