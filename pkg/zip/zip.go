package zip

import (
	"github.com/mholt/archiver/v3"
)

const cachePrefix = "cogcache"
const hashLength = 64

type CachingZip struct {
	zip *archiver.Zip
}

func NewCachingZip() *CachingZip {
	return &CachingZip{zip: &archiver.Zip{ImplicitTopLevelFolder: false}}
}
