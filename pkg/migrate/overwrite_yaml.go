package migrate

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func OverwrightYAML(sourceYaml []byte, destinationYaml []byte) ([]byte, error) {
	var sourceNode yaml.Node
	err := yaml.Unmarshal(sourceYaml, &sourceNode)
	if err != nil {
		return nil, err
	}

	var destinationNode yaml.Node
	err = yaml.Unmarshal(destinationYaml, &destinationNode)
	if err != nil {
		return nil, err
	}

	err = traverseAndCompare(sourceNode.Content[0], destinationNode.Content[0], "")
	if err != nil {
		return nil, err
	}

	return yaml.Marshal(&destinationNode)
}

func traverseAndCompare(sourceNode, destinationNode *yaml.Node, path string) error {
	if sourceNode.Kind != destinationNode.Kind {
		return fmt.Errorf("Type mismatch at %s: %s vs %s\n", path, nodeKindToString(sourceNode.Kind), nodeKindToString(destinationNode.Kind))
	}

	switch sourceNode.Kind {
	case yaml.ScalarNode:
		if sourceNode.Value != destinationNode.Value {
			destinationNode.Value = sourceNode.Value
		}

	case yaml.MappingNode:
		map1 := mapNodeToMap(sourceNode)
		map2 := mapNodeToMap(destinationNode)

		allKeys := getAllKeys(map1, map2)

		for _, key := range allKeys {
			var childPath string
			if path == "" {
				childPath = key
			} else {
				childPath = path + "." + key
			}

			sourceNodeChild, ok1 := map1[key]
			destinationNodeChild, ok2 := map2[key]

			if !ok1 || !ok2 {
				destinationNode.Content = sourceNode.Content
			} else {
				err := traverseAndCompare(sourceNodeChild, destinationNodeChild, childPath)
				if err != nil {
					return err
				}
			}
		}

	case yaml.SequenceNode:
		sourceLen := len(sourceNode.Content)
		destinationLen := len(destinationNode.Content)

		maxLen := sourceLen
		if destinationLen > maxLen {
			maxLen = destinationLen
		}

		for i := 0; i < maxLen; i++ {
			childPath := fmt.Sprintf("%s[%d]", path, i)

			if i >= destinationLen {
				destinationNode.Content = append(destinationNode.Content, sourceNode.Content[i])
			} else if i < sourceLen {
				err := traverseAndCompare(sourceNode.Content[i], destinationNode.Content[i], childPath)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func mapNodeToMap(node *yaml.Node) map[string]*yaml.Node {
	result := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		result[keyNode.Value] = valueNode
	}
	return result
}

func getAllKeys(map1, map2 map[string]*yaml.Node) []string {
	keys := make(map[string]struct{})
	for key := range map1 {
		keys[key] = struct{}{}
	}
	for key := range map2 {
		keys[key] = struct{}{}
	}

	var keyList []string
	for key := range keys {
		keyList = append(keyList, key)
	}
	return keyList
}

func nodeKindToString(kind yaml.Kind) string {
	switch kind {
	case yaml.ScalarNode:
		return "Scalar"
	case yaml.MappingNode:
		return "Mapping"
	case yaml.SequenceNode:
		return "Sequence"
	default:
		return "Unknown"
	}
}
