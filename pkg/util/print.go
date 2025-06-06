package util

import (
	"encoding/json"
	"fmt"
)

func PrettyPrintJSON(thing any) {
	json, _ := json.MarshalIndent(thing, "", "  ")
	fmt.Println(string(json))
}
