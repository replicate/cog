package util

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func OverwriteYAML(sourceYaml []byte, destinationYaml []byte) ([]byte, error) {
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
	sourceNode.LineComment = destinationNode.LineComment
	sourceNode.HeadComment = destinationNode.HeadComment
	sourceNode.FootComment = destinationNode.FootComment

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

			sourceKVNodeChild, ok1 := map1[key]
			destinationKVNodeChild, ok2 := map2[key]

			switch {
			case !ok1:
				// We need to remove this node
				NewContent := []*yaml.Node{}
				for _, node := range destinationNode.Content {
					if node == destinationKVNodeChild[0] || node == destinationKVNodeChild[1] {
						continue
					}
					NewContent = append(NewContent, node)
				}
				destinationNode.Content = NewContent
			case !ok2:
				// We need to add this node
				destinationNode.Content = append(destinationNode.Content, sourceKVNodeChild...)
			default:
				err := traverseAndCompare(sourceKVNodeChild[1], destinationKVNodeChild[1], childPath)
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

func mapNodeToMap(node *yaml.Node) map[string][]*yaml.Node {
	result := make(map[string][]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		result[keyNode.Value] = []*yaml.Node{keyNode, valueNode}
	}
	return result
}

func getAllKeys(map1, map2 map[string][]*yaml.Node) []string {
	keys := make(map[string]bool)
	for key := range map1 {
		keys[key] = true
	}
	for key := range map2 {
		keys[key] = true
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
