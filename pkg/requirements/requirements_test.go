package requirements

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPythonRequirements(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("torch==2.5.1"), 0o644)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	requirementsFile, err := GenerateRequirements(tmpDir, reqFile, RequirementsFile)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tmpDir, "requirements.txt"), requirementsFile)
}

func TestReadRequirements(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("torch==2.5.1"), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{"torch==2.5.1"}, requirements)
}

func TestReadRequirementsLineContinuations(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("torch==\\\n2.5.1\ntorchvision==\\\r\n2.5.1"), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{"torch==2.5.1", "torchvision==2.5.1"}, requirements)
}

func TestReadRequirementsStripComments(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("torch==\\\n2.5.1# Heres my comment\ntorchvision==2.5.1\n# Heres a beginning of line comment"), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{"torch==2.5.1", "torchvision==2.5.1"}, requirements)
}

func TestReadRequirementsComplex(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte(`foo==1.0.0
# complex requirements
fastapi>=0.6,<1
flask>0.4
# comments!
# blank lines!

# arguments
-f http://example.com`), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{"foo==1.0.0", "fastapi>=0.6,<1", "flask>0.4", "-f http://example.com"}, requirements)
}

func TestReadRequirementsLongLine(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte(`
antlr4-python3-runtime==4.9.3 \
    --hash=sha256:f224469b4168294902bb1efa80a8bf7855f24c99aef99cbefc1bcd3cce77881b
colorama==0.4.6 ; sys_platform == 'win32' \
    --hash=sha256:08695f5cb7ed6e0531a20572697297273c47b8cae5a63ffc6d6ed5c201be6e44 \
    --hash=sha256:4f1d9991f5acc0ca119f9d443620b77f9d6b33703e51011c16baf57afb285fc6
contourpy==1.3.2 \
    --hash=sha256:0475b1f6604896bc7c53bb070e355e9321e1bc0d381735421a2d2068ec56531f \
    --hash=sha256:106fab697af11456fcba3e352ad50effe493a90f893fca6c2ca5c033820cea92 \
    --hash=sha256:15ce6ab60957ca74cff444fe66d9045c1fd3e92c8936894ebd1f3eef2fff075f \
    --hash=sha256:1c48188778d4d2f3d48e4643fb15d8608b1d01e4b4d6b0548d9b336c28fc9b6f \
    --hash=sha256:3859783aefa2b8355697f16642695a5b9792e7a46ab86da1118a4a23a51a33d7 \
    --hash=sha256:3d80b2c0300583228ac98d0a927a1ba6a2ba6b8a742463c564f1d419ee5b211e \
    --hash=sha256:3f9e896f447c5c8618f1edb2bafa9a4030f22a575ec418ad70611450720b5b08 \
    --hash=sha256:434f0adf84911c924519d2b08fc10491dd282b20bdd3fa8f60fd816ea0b48841 \
    --hash=sha256:49b65a95d642d4efa8f64ba12558fcb83407e58a2dfba9d796d77b63ccfcaff5 \
    --hash=sha256:4caf2bcd2969402bf77edc4cb6034c7dd7c0803213b3523f111eb7460a51b8d2 \
    --hash=sha256:532fd26e715560721bb0d5fc7610fce279b3699b018600ab999d1be895b09415 \
    --hash=sha256:5ebac872ba09cb8f2131c46b8739a7ff71de28a24c869bcad554477eb089a878 \
    --hash=sha256:5f5964cdad279256c084b69c3f412b7801e15356b16efa9d78aa974041903da0 \
    --hash=sha256:65a887a6e8c4cd0897507d814b14c54a8c2e2aa4ac9f7686292f9769fcf9a6ab \
    --hash=sha256:6a37a2fb93d4df3fc4c0e363ea4d16f83195fc09c891bc8ce072b9d084853445 \
    --hash=sha256:70771a461aaeb335df14deb6c97439973d253ae70660ca085eec25241137ef43 \
    --hash=sha256:71e2bd4a1c4188f5c2b8d274da78faab884b59df20df63c34f74aa1813c4427c \
    --hash=sha256:745b57db7758f3ffc05a10254edd3182a2a83402a89c00957a8e8a22f5582823 \
    --hash=sha256:78e9253c3de756b3f6a5174d024c4835acd59eb3f8e2ca13e775dbffe1558f69 \
    --hash=sha256:82199cb78276249796419fe36b7386bd8d2cc3f28b3bc19fe2454fe2e26c4c15 \
    --hash=sha256:8b7fc0cd78ba2f4695fd0a6ad81a19e7e3ab825c31b577f384aa9d7817dc3bef \
    --hash=sha256:8c5acb8dddb0752bf252e01a3035b21443158910ac16a3b0d20e7fed7d534ce5 \
    --hash=sha256:8c942a01d9163e2e5cfb05cb66110121b8d07ad438a17f9e766317bcb62abf73 \
    --hash=sha256:90df94c89a91b7362e1142cbee7568f86514412ab8a2c0d0fca72d7e91b62912 \
    --hash=sha256:970e9173dbd7eba9b4e01aab19215a48ee5dd3f43cef736eebde064a171f89a5 \
    --hash=sha256:977e98a0e0480d3fe292246417239d2d45435904afd6d7332d8455981c408b85 \
    --hash=sha256:b6945942715a034c671b7fc54f9588126b0b8bf23db2696e3ca8328f3ff0ab54 \
    --hash=sha256:b7cd50c38f500bbcc9b6a46643a40e0913673f869315d8e70de0438817cb7773 \
    --hash=sha256:c49f73e61f1f774650a55d221803b101d966ca0c5a2d6d5e4320ec3997489441 \
    --hash=sha256:c66c4906cdbc50e9cba65978823e6e00b45682eb09adbb78c9775b74eb222422 \
    --hash=sha256:c6c4639a9c22230276b7bffb6a850dfc8258a2521305e1faefe804d006b2e532 \
    --hash=sha256:c85bb486e9be652314bb5b9e2e3b0d1b2e643d5eec4992c0fbe8ac71775da739 \
    --hash=sha256:cc829960f34ba36aad4302e78eabf3ef16a3a100863f0d4eeddf30e8a485a03b \
    --hash=sha256:d0e589ae0d55204991450bb5c23f571c64fe43adaa53f93fc902a84c96f52fe1 \
    --hash=sha256:d14f12932a8d620e307f715857107b1d1845cc44fdb5da2bc8e850f5ceba9f87 \
    --hash=sha256:d32530b534e986374fc19eaa77fcb87e8a99e5431499949b828312bdcd20ac52 \
    --hash=sha256:d6658ccc7251a4433eebd89ed2672c2ed96fba367fd25ca9512aa92a4b46c4f1 \
    --hash=sha256:d91a3ccc7fea94ca0acab82ceb77f396d50a1f67412efe4c526f5d20264e6ecd \
    --hash=sha256:de39db2604ae755316cb5967728f4bea92685884b1e767b7c24e983ef5f771cb \
    --hash=sha256:de425af81b6cea33101ae95ece1f696af39446db9682a0b56daaa48cfc29f38f \
    --hash=sha256:e1578f7eafce927b168752ed7e22646dad6cd9bca673c60bff55889fa236ebf9 \
    --hash=sha256:e298e7e70cf4eb179cc1077be1c725b5fd131ebc81181bf0c03525c8abc297fd \
    --hash=sha256:eab0f6db315fa4d70f1d8ab514e527f0366ec021ff853d7ed6a2d33605cf4b83 \
    --hash=sha256:f26b383144cf2d2c29f01a1e8170f50dacf0eac02d64139dcd709a8ac4eb3cfe
cycler==0.12.1 \
    --hash=sha256:85cef7cff222d8644161529808465972e51340599459b8ac3ccbac5a854e0d30 \
    --hash=sha256:88bb128f02ba341da8ef447245a9e138fae777f6a23943da4540077d3601eb1c`), 0o644)
	require.NoError(t, err)
	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	checkRequirements(t, []string{
		"antlr4-python3-runtime==4.9.3 --hash=sha256:f224469b4168294902bb1efa80a8bf7855f24c99aef99cbefc1bcd3cce77881b",
		"colorama==0.4.6 ; sys_platform == 'win32' --hash=sha256:08695f5cb7ed6e0531a20572697297273c47b8cae5a63ffc6d6ed5c201be6e44 --hash=sha256:4f1d9991f5acc0ca119f9d443620b77f9d6b33703e51011c16baf57afb285fc6",
		"contourpy==1.3.2 --hash=sha256:0475b1f6604896bc7c53bb070e355e9321e1bc0d381735421a2d2068ec56531f --hash=sha256:106fab697af11456fcba3e352ad50effe493a90f893fca6c2ca5c033820cea92 --hash=sha256:15ce6ab60957ca74cff444fe66d9045c1fd3e92c8936894ebd1f3eef2fff075f --hash=sha256:1c48188778d4d2f3d48e4643fb15d8608b1d01e4b4d6b0548d9b336c28fc9b6f --hash=sha256:3859783aefa2b8355697f16642695a5b9792e7a46ab86da1118a4a23a51a33d7 --hash=sha256:3d80b2c0300583228ac98d0a927a1ba6a2ba6b8a742463c564f1d419ee5b211e --hash=sha256:3f9e896f447c5c8618f1edb2bafa9a4030f22a575ec418ad70611450720b5b08 --hash=sha256:434f0adf84911c924519d2b08fc10491dd282b20bdd3fa8f60fd816ea0b48841 --hash=sha256:49b65a95d642d4efa8f64ba12558fcb83407e58a2dfba9d796d77b63ccfcaff5 --hash=sha256:4caf2bcd2969402bf77edc4cb6034c7dd7c0803213b3523f111eb7460a51b8d2 --hash=sha256:532fd26e715560721bb0d5fc7610fce279b3699b018600ab999d1be895b09415 --hash=sha256:5ebac872ba09cb8f2131c46b8739a7ff71de28a24c869bcad554477eb089a878 --hash=sha256:5f5964cdad279256c084b69c3f412b7801e15356b16efa9d78aa974041903da0 --hash=sha256:65a887a6e8c4cd0897507d814b14c54a8c2e2aa4ac9f7686292f9769fcf9a6ab --hash=sha256:6a37a2fb93d4df3fc4c0e363ea4d16f83195fc09c891bc8ce072b9d084853445 --hash=sha256:70771a461aaeb335df14deb6c97439973d253ae70660ca085eec25241137ef43 --hash=sha256:71e2bd4a1c4188f5c2b8d274da78faab884b59df20df63c34f74aa1813c4427c --hash=sha256:745b57db7758f3ffc05a10254edd3182a2a83402a89c00957a8e8a22f5582823 --hash=sha256:78e9253c3de756b3f6a5174d024c4835acd59eb3f8e2ca13e775dbffe1558f69 --hash=sha256:82199cb78276249796419fe36b7386bd8d2cc3f28b3bc19fe2454fe2e26c4c15 --hash=sha256:8b7fc0cd78ba2f4695fd0a6ad81a19e7e3ab825c31b577f384aa9d7817dc3bef --hash=sha256:8c5acb8dddb0752bf252e01a3035b21443158910ac16a3b0d20e7fed7d534ce5 --hash=sha256:8c942a01d9163e2e5cfb05cb66110121b8d07ad438a17f9e766317bcb62abf73 --hash=sha256:90df94c89a91b7362e1142cbee7568f86514412ab8a2c0d0fca72d7e91b62912 --hash=sha256:970e9173dbd7eba9b4e01aab19215a48ee5dd3f43cef736eebde064a171f89a5 --hash=sha256:977e98a0e0480d3fe292246417239d2d45435904afd6d7332d8455981c408b85 --hash=sha256:b6945942715a034c671b7fc54f9588126b0b8bf23db2696e3ca8328f3ff0ab54 --hash=sha256:b7cd50c38f500bbcc9b6a46643a40e0913673f869315d8e70de0438817cb7773 --hash=sha256:c49f73e61f1f774650a55d221803b101d966ca0c5a2d6d5e4320ec3997489441 --hash=sha256:c66c4906cdbc50e9cba65978823e6e00b45682eb09adbb78c9775b74eb222422 --hash=sha256:c6c4639a9c22230276b7bffb6a850dfc8258a2521305e1faefe804d006b2e532 --hash=sha256:c85bb486e9be652314bb5b9e2e3b0d1b2e643d5eec4992c0fbe8ac71775da739 --hash=sha256:cc829960f34ba36aad4302e78eabf3ef16a3a100863f0d4eeddf30e8a485a03b --hash=sha256:d0e589ae0d55204991450bb5c23f571c64fe43adaa53f93fc902a84c96f52fe1 --hash=sha256:d14f12932a8d620e307f715857107b1d1845cc44fdb5da2bc8e850f5ceba9f87 --hash=sha256:d32530b534e986374fc19eaa77fcb87e8a99e5431499949b828312bdcd20ac52 --hash=sha256:d6658ccc7251a4433eebd89ed2672c2ed96fba367fd25ca9512aa92a4b46c4f1 --hash=sha256:d91a3ccc7fea94ca0acab82ceb77f396d50a1f67412efe4c526f5d20264e6ecd --hash=sha256:de39db2604ae755316cb5967728f4bea92685884b1e767b7c24e983ef5f771cb --hash=sha256:de425af81b6cea33101ae95ece1f696af39446db9682a0b56daaa48cfc29f38f --hash=sha256:e1578f7eafce927b168752ed7e22646dad6cd9bca673c60bff55889fa236ebf9 --hash=sha256:e298e7e70cf4eb179cc1077be1c725b5fd131ebc81181bf0c03525c8abc297fd --hash=sha256:eab0f6db315fa4d70f1d8ab514e527f0366ec021ff853d7ed6a2d33605cf4b83 --hash=sha256:f26b383144cf2d2c29f01a1e8170f50dacf0eac02d64139dcd709a8ac4eb3cfe",
		"cycler==0.12.1 --hash=sha256:85cef7cff222d8644161529808465972e51340599459b8ac3ccbac5a854e0d30 --hash=sha256:88bb128f02ba341da8ef447245a9e138fae777f6a23943da4540077d3601eb1c",
	}, requirements)
}

func TestComfyUIRequirements(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte(`torch
torchvision
torchaudio
torchsde
einops
transformers>=4.49.0
tokenizers>=0.13.3
sentencepiece
safetensors>=0.3.0
aiohttp
accelerate>=1.1.1
pyyaml
Pillow
scipy
tqdm
psutil
spandrel
soundfile
kornia>=0.7.1
websocket-client==1.6.3
diffusers>=0.31.0
av>=14.1.0
comfyui-frontend-package==1.17.11
comfyui-workflow-templates==0.1.3

# ComfyUI-AdvancedLivePortrait
dill

# Inspire
webcolors

# fix for pydantic issues in cog
# https://github.com/replicate/cog/issues/1623
albumentations==1.4.3

# was-node-suite-comfyui
# https://github.com/WASasquatch/was-node-suite-comfyui/blob/main/requirements.txt
cmake
imageio
joblib
matplotlib
pilgram
scikit-learn
rembg

# ComfyUI_essentials
numba

# ComfyUI_FizzNodes
pandas
numexpr

# comfyui-reactor-node
insightface
onnx

# ComfyUI-Impact-Pack
segment-anything
piexif

# ComfyUI-Impact-Subpack
ultralytics!=8.0.177

# comfyui_segment_anything
timm

# comfyui_controlnet_aux
# https://github.com/Fannovel16/comfyui_controlnet_aux/blob/main/requirements.txt
importlib_metadata
opencv-python-headless>=4.0.1.24
filelock
numpy
scikit-image
python-dateutil
mediapipe
svglib
fvcore
yapf
omegaconf
ftfy
addict
yacs
trimesh[easy]

# ComfyUI-KJNodes
librosa
color-matcher

# PuLID
facexlib

# SUPIR
open-clip-torch>=2.24.0
pytorch-lightning>=2.2.1

# For train.py and custom loras
huggingface_hub[hf-transfer]

# ComfyUI-segment-anything-2
iopath`), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{
		"torch",
		"torchvision",
		"torchaudio",
		"torchsde",
		"einops",
		"transformers>=4.49.0",
		"tokenizers>=0.13.3",
		"sentencepiece",
		"safetensors>=0.3.0",
		"aiohttp",
		"accelerate>=1.1.1",
		"pyyaml",
		"Pillow",
		"scipy",
		"tqdm",
		"psutil",
		"spandrel",
		"soundfile",
		"kornia>=0.7.1",
		"websocket-client==1.6.3",
		"diffusers>=0.31.0",
		"av>=14.1.0",
		"comfyui-frontend-package==1.17.11",
		"comfyui-workflow-templates==0.1.3",
		"dill",
		"webcolors",
		"albumentations==1.4.3",
		"cmake",
		"imageio",
		"joblib",
		"matplotlib",
		"pilgram",
		"scikit-learn",
		"rembg",
		"numba",
		"pandas",
		"numexpr",
		"insightface",
		"onnx",
		"segment-anything",
		"piexif",
		"ultralytics!=8.0.177",
		"timm",
		"importlib_metadata",
		"opencv-python-headless>=4.0.1.24",
		"filelock",
		"numpy",
		"scikit-image",
		"python-dateutil",
		"mediapipe",
		"svglib",
		"fvcore",
		"yapf",
		"omegaconf",
		"ftfy",
		"addict",
		"yacs",
		"trimesh[easy]",
		"librosa",
		"color-matcher",
		"facexlib",
		"open-clip-torch>=2.24.0",
		"pytorch-lightning>=2.2.1",
		"huggingface_hub[hf-transfer]",
		"iopath",
	}, requirements)
}

func TestTensorflowRequirements(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, ".requirements.txt")
	err := os.WriteFile(reqFile, []byte(`compel==2.0.3
diffusers>=0.27.1
gputil==1.4.0
loguru==0.7.2
opencv-python>=4.9.0.80
pillow>=10.2.0
psutil==6.1.1
replicate>=1.0.4
sentry-sdk[fastapi,loguru]>=2.16.0
antialiased_cnns==0.3
beautifulsoup4==4.13.4
imageio==2.37.0
ipdb==0.13.13
kornia==0.8.1
matplotlib==3.10.3
numpy==1.23.5
opencv_python==4.11.0.86
Pillow==11.2.1
pytorch_lightning==2.3.3
PyYAML==6.0.2
Requests==2.32.3
scipy==1.15.3
scikit-image==0.24.0
tensorflow==2.10.0
tensorlayer==2.2.5
tf_slim==1.1.0
timm==1.0.15
torch==2.0.1
torchvision==0.15.2
tqdm==4.67.1`), 0o644)
	require.NoError(t, err)
	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{
		"compel==2.0.3",
		"diffusers>=0.27.1",
		"gputil==1.4.0",
		"loguru==0.7.2",
		"opencv-python>=4.9.0.80",
		"pillow>=10.2.0",
		"psutil==6.1.1",
		"replicate>=1.0.4",
		"sentry-sdk[fastapi,loguru]>=2.16.0",
		"antialiased_cnns==0.3",
		"beautifulsoup4==4.13.4",
		"imageio==2.37.0",
		"ipdb==0.13.13",
		"kornia==0.8.1",
		"matplotlib==3.10.3",
		"numpy==1.23.5",
		"opencv_python==4.11.0.86",
		"Pillow==11.2.1",
		"pytorch_lightning==2.3.3",
		"PyYAML==6.0.2",
		"Requests==2.32.3",
		"scipy==1.15.3",
		"scikit-image==0.24.0",
		"tensorflow==2.10.0",
		"tensorlayer==2.2.5",
		"tf_slim==1.1.0",
		"timm==1.0.15",
		"torch==2.0.1",
		"torchvision==0.15.2",
		"tqdm==4.67.1",
	}, requirements)
}

func TestSplitPinnedPythonRequirement(t *testing.T) {
	testCases := []struct {
		input                  string
		expectedName           string
		expectedVersion        string
		expectedFindLinks      []string
		expectedExtraIndexURLs []string
		expectedError          bool
	}{
		{"package1==1.0.0", "package1", "1.0.0", nil, nil, false},
		{"package1==1.0.0+alpha", "package1", "1.0.0+alpha", nil, nil, false},
		{"--find-links=link1 --find-links=link2 package3==3.0.0", "package3", "3.0.0", []string{"link1", "link2"}, nil, false},
		{"package4==4.0.0 --extra-index-url=url1 --extra-index-url=url2", "package4", "4.0.0", nil, []string{"url1", "url2"}, false},
		{"-f link1 --find-links=link2 package5==5.0.0 --extra-index-url=url1 --extra-index-url=url2", "package5", "5.0.0", []string{"link1", "link2"}, []string{"url1", "url2"}, false},
		{"package6 --find-links=link1 --find-links=link2 --extra-index-url=url1 --extra-index-url=url2", "", "", nil, nil, true},
		{"invalid package", "", "", nil, nil, true},
		{"package8==", "", "", nil, nil, true},
		{"==8.0.0", "", "", nil, nil, true},
	}

	for _, tc := range testCases {
		name, version, findLinks, extraIndexURLs, err := SplitPinnedPythonRequirement(tc.input)

		if tc.expectedError {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.expectedName, name, "input: "+tc.input)
			require.Equal(t, tc.expectedVersion, version, "input: "+tc.input)
			require.Equal(t, tc.expectedFindLinks, findLinks, "input: "+tc.input)
			require.Equal(t, tc.expectedExtraIndexURLs, extraIndexURLs, "input: "+tc.input)
		}
	}
}

func TestReadRequirementsWithEditable(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("-e .\ntorch==2.5.1"), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{"torch==2.5.1"}, requirements)
}

func TestVersionSpecifier(t *testing.T) {
	specifier := VersionSpecifier("mypackage>= 1.0, < 1.4 || > 2.0")
	require.Equal(t, specifier, ">=1.0,<1.4||>2.0")
}

func TestPackageName(t *testing.T) {
	name := PackageName("mypackage>= 1.0, < 1.4 || > 2.0")
	require.Equal(t, name, "mypackage")
}

func TestVersions(t *testing.T) {
	versions := Versions("another @ https://some.domain/package.whl")
	require.Equal(t, versions, []string{"https://some.domain/package.whl"})
}

func checkRequirements(t *testing.T, expected []string, actual []string) {
	t.Helper()
	for n, expectLine := range expected {
		actualLine := actual[n]
		// collapse any multiple-space runs with single spaces in the actual line - the generator may output these
		// but we don't care about them for comparison purposes
		actualLine = strings.Join(strings.Fields(actualLine), " ")
		require.Equal(t, expectLine, actualLine)
	}
	require.Equal(t, len(expected), len(actual))
}
