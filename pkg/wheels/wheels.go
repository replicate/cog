package wheels

import _ "embed"

//go:generate ../../script/generate-wheels

//go:embed cog.whl
var cogWheel []byte

//go:embed coglet.whl
var cogletWheel []byte

func ReadCogWheel() (string, []byte) {
	return "cog.whl", cogWheel
}

func ReadCogletWheel() (string, []byte) {
	return "coglet.whl", cogletWheel
}
