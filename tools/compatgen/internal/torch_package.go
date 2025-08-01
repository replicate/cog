package internal

type TorchPackage struct {
	Name          string
	Version       string
	Variant       string
	CUDA          *string
	PythonVersion string
}

func (c *TorchPackage) Equals(other TorchPackage) bool {
	if c.CUDA != other.CUDA {
		if c.CUDA != nil && other.CUDA != nil && *c.CUDA != *other.CUDA {
			return false
		}
	}
	return c.Name == other.Name && c.Version == other.Version && c.Variant == other.Variant && c.PythonVersion == other.PythonVersion
}
