package docker

func ImageExists(id string) (bool, error) {
	_, err := ImageInspect(id)
	if err == ErrNoSuchImage {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
