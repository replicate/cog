// Code generated for package config by go-bindata DO NOT EDIT. (@generated)
// sources:
// data/config_schema_v1.0.json
package config

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func bindataRead(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	clErr := gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}
	if clErr != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _dataConfig_schema_v10Json = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x84\x93\x41\x6f\xd3\x4e\x10\xc5\xef\xf9\x14\x4f\xfb\xff\x4b\x50\x29\x75\x40\x3d\x50\xe5\x86\xe0\x82\x04\x52\x0f\xbd\xa0\xaa\x42\x1b\xef\xd8\x9e\x62\xef\x9a\xdd\x71\x4b\x88\xfa\xdd\x91\xd7\xeb\xd4\x4e\x0c\x5c\x56\xc9\xcc\x9b\x37\xe3\xf9\xed\x1e\x56\x80\xfa\x3f\xe4\x15\x35\x5a\x6d\xa1\x2a\x91\x76\xbb\xd9\x3c\x04\x67\x2f\x87\x68\xe6\x7c\xb9\x31\x5e\x17\x72\xf9\xe6\xdd\x26\x29\xd7\xb1\x8c\xcd\xa4\x84\x7e\xea\xa6\xad\x29\xcb\x5d\x73\xfc\xdd\xdb\x0c\x5a\xd9\xb7\xd4\x8b\xdd\xee\x81\x72\x49\x31\x96\x3a\x06\x3f\xb8\xf2\x55\x40\xee\x6c\xc1\x25\xbe\xbe\xff\xf2\x19\xd3\x36\x86\x42\xee\xb9\x15\x76\xb6\x17\xdf\x56\x1c\x52\x1e\x1c\xd0\x05\x32\x10\x87\x47\x5d\xb3\xd1\x42\xc8\x5d\x99\xed\x75\x53\xa3\xe0\x9a\xc0\x16\xa4\xf3\x0a\xad\x77\x7d\xe3\xec\x98\x35\x54\xb0\xa5\x80\xca\x3d\xf5\xe5\xbb\x8e\x6b\x03\x8d\x8f\x2e\xff\x4e\x1e\xdc\xe8\x92\xa0\xad\x19\xf3\xbe\xb3\x68\x3d\x19\xce\xfb\x39\x02\x9c\xc5\xde\x75\x1e\x8d\x33\x54\x83\x6d\x60\x43\x90\x4a\xcb\x50\x9a\x8d\xa3\x17\xba\xab\x45\x6d\x71\x78\x8e\x81\xb4\x98\xa0\xb6\xb8\x5b\x01\xc0\x21\x9e\x80\x8a\x03\xf4\xc2\x14\x00\x54\xbb\x97\xca\xd9\x6f\x8f\xe4\x43\xfa\xf6\xab\xec\x5a\xa5\xfc\xf3\x6a\x3c\xef\xa3\xb3\xa7\x1f\x1d\x7b\x32\x47\xe7\xe4\x78\x14\xb4\xde\xb5\xe4\x85\x63\xf3\xc3\x6a\xb1\xe9\x88\xf4\xbf\xcd\x8b\x7a\x33\x88\xd6\xa3\x64\x81\xe4\x10\x1f\x69\xde\x56\x94\xd6\x39\xa1\x18\x25\x8b\x24\x45\xdb\x5f\x1a\x43\x6a\x77\x0a\x44\x2a\x9a\x23\x99\x2c\xdd\x77\x36\x80\x6d\x86\x4f\xd2\xdf\x1d\xd1\x6c\x03\x1e\xb5\x67\xd7\x05\xb8\x76\xe0\xf4\xc4\x52\xb1\x05\xcb\x76\x3a\xc6\x9c\x4a\x0c\x9e\x91\x99\xd2\xf9\x37\x8e\x11\x48\xda\x76\x2c\x38\x43\xb2\x64\x73\x56\xb5\xc0\x69\xb9\xfd\x6c\xb8\x3f\x82\x9b\x06\x4e\x2c\xd6\x53\x83\x84\xf5\x4e\xd9\xae\xd9\x91\x57\x6b\xa8\x20\x9e\x6d\xa9\xee\xe7\xba\x29\xe6\xb9\xe1\x29\xef\x65\xe6\x84\x86\xad\xf3\x78\x7d\x95\x5d\x5f\xc0\x79\xb4\x5a\xf2\x2a\xfe\xcd\xde\x5e\x60\xf4\x72\x05\x6e\xa2\x7b\x7f\x19\xba\x90\x5e\xd4\xc4\x76\x64\xa8\xe6\x89\x45\x8e\x31\x33\xa3\x05\xdc\x9f\x91\x7b\xb9\x0c\xda\x18\xee\x27\xd6\xf5\xcd\x94\x86\xf8\x8e\x8e\xcf\x6e\x78\xd0\x7f\x53\x3e\xff\x0e\x00\x00\xff\xff\x44\xbe\x42\x34\x5d\x05\x00\x00")

func dataConfig_schema_v10JsonBytes() ([]byte, error) {
	return bindataRead(
		_dataConfig_schema_v10Json,
		"data/config_schema_v1.0.json",
	)
}

func dataConfig_schema_v10Json() (*asset, error) {
	bytes, err := dataConfig_schema_v10JsonBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "data/config_schema_v1.0.json", size: 1373, mode: os.FileMode(420), modTime: time.Unix(1635947718, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"data/config_schema_v1.0.json": dataConfig_schema_v10Json,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"data": &bintree{nil, map[string]*bintree{
		"config_schema_v1.0.json": &bintree{dataConfig_schema_v10Json, map[string]*bintree{}},
	}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
