package docker

func ImageExists(id string) (bool, error) {
	_, err := ImageInspect(id)
	// assume all errors mean it doesn't exist
	// TODO(andreas): differentiate between actual errors and image not
	// existing in ImageInspect
	if err != nil {
		return false, nil
	}
	return true, nil
}
