package nix

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
)

// getTag retrieves the repo tag from the specified command file.
// It reads the command file, extracts the JSON path, reads the JSON content,
// and returns the value of the "repo_tag" field from the JSON data.
func getTag(cmd string) (string, error) {
	// can't the repo_tag be read from the build output or from the binary?
	fileContent, err := os.ReadFile(cmd)
	if err != nil {
		return "", err
	}
	jsonPathRegex := regexp.MustCompile(`(/nix/store/[-.+_0-9a-zA-Z]+\.json)`)
	jsonPath := jsonPathRegex.FindStringSubmatch(string(fileContent))[1]
	jsonContent, err := os.ReadFile(jsonPath)
	if err != nil {
		return "", err
	}
	var jsonData map[string]interface{}
	err = json.Unmarshal(jsonContent, &jsonData)
	if err != nil {
		return "", err
	}
	return jsonData["repo_tag"].(string), nil
}

// callNix executes a Nix command with the specified target and extra flags.
// If captureOutput is true, the command's output is captured and printed.
// Otherwise, the command's output is directed to os.Stdout and os.Stderr.
// Returns an error if the command execution fails.
func callNix(command string, target string, extraFlags []string) ([]byte, error) {
	invocation := []string{"nix", command, ".#" + target}
	invocation = append(invocation, extraFlags...)
	fmt.Println("Invoking:", invocation)
	cmd := exec.Command(invocation[0], invocation[1:]...)
	return cmd.Output()
}

//([]map[string]interface{}, error) {

func lock() ([]byte, error) {
	return callNix("run", "", []string{"lock"})
}

func build() ([]map[string]interface{}, error) {
	output, err := callNix("build", "", []string{"--json", "--no-link"})
	if output != nil {
		fmt.Println("Build output:", output)
	}
	if err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	err = json.Unmarshal(output, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// build the image streaming binary and call it to load into docker
// build the image, get the tag, check if it's already loaded, load it if not, otherwise
// call the binary that was built by nix to handle writing to the local docker daemon
// return the tag of the image
func load() (string, error) {
	result, err := build()
	if err != nil {
		return "", err
	}
	cmd := result[0]["outputs"].(map[string]interface{})["out"].(string)
	tag, err := getTag(cmd)
	if err != nil {
		return "", err
	}
	err = exec.Command("docker", "image", "inspect", tag).Run()
	if err == nil {
		fmt.Println("Already loaded into docker:", tag)
		return tag, nil
	}
	// load has a --tag argment that should be exposed
	err = exec.Command("sh", "-c", fmt.Sprintf("%s load", cmd)).Run()
	if err != nil {
		return "", err
	}
	return tag, nil
}

func NixPush(token string, url string) error {
	if _, err := lock(); err != nil {
		fmt.Println("lock err", err)
		return err
	}
	fmt.Println("Invoking: git add lock.json")
	exec.Command("git", "add", "lock.json").Run()
	result, err := build()
	if err != nil {
		fmt.Println("build error", err)
		return err
	}
	cmd := result[0]["outputs"].(map[string]interface{})["out"].(string)
	cmdExec := exec.Command(cmd, "push", "-t", token, url)
	cmdExec.Stderr = os.Stderr
	cmdExec.Stdout = os.Stdout
	return cmdExec.Run()
}
