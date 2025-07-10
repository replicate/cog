package util

import (
	"encoding/json"
	"os"
)

func JSONPrettyPrint(thing any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(thing); err != nil {
		panic(err)
	}
}
