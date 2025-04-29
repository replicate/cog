package docker

import "context"

func ImageExists(ctx context.Context, id string) (bool, error) {
	_, err := ImageInspect(ctx, id)
	if err == ErrNoSuchImage {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
