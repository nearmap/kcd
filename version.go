package main

import (
	"io/ioutil"
)

var Version string

func init() {
	v, err := ioutil.ReadFile("version")
	if err == nil {
		Version = string(v)
	}
}
