package internal

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/replicate/cog/pkg/util/version"

	"github.com/anaskhan96/soup"

	"github.com/replicate/cog/pkg/config"
)

func FetchTensorFlowCompatibilityMatrix() ([]config.TFCompatibility, error) {
	url := "https://www.tensorflow.org/install/source"
	minCudaVersion := strconv.Itoa(config.MinimumMajorCudaVersion)

	resp, err := soup.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Failed to download %s: %w", url, err)
	}

	doc := soup.HTMLParse(resp)
	gpuHeading := doc.Find("h4", "id", "gpu")
	table := gpuHeading.FindNextElementSibling()
	rows := table.FindAll("tr")

	compats := []config.TFCompatibility{}
	for _, row := range rows[1:] {
		cells := row.FindAll("td")
		gpuPackage, packageVersion := split2(cells[0].Text(), "-")
		pythonVersions, err := parsePythonVersionsCell(cells[1].Text())
		if err != nil {
			return nil, err
		}
		cuDNN := cells[4].Text()
		cuda := cells[5].Text()

		if !version.Greater(cuda, minCudaVersion) && !version.Equal(cuda, minCudaVersion) {
			continue
		}

		compat := config.TFCompatibility{
			TF:           packageVersion,
			TFCPUPackage: "tensorflow==" + packageVersion,
			TFGPUPackage: gpuPackage + "==" + packageVersion,
			CUDA:         cuda,
			CuDNN:        cuDNN,
			Pythons:      pythonVersions,
		}
		compats = append(compats, compat)
	}

	// sanity check
	if len(compats) < 12 {
		return nil, fmt.Errorf("Tensorflow compatibility matrix only had %d rows, has the html changed?", len(compats))
	}

	return compats, nil
}

func parsePythonVersionsCell(val string) ([]string, error) {
	versions := []string{}
	parts := strings.Split(val, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			start, end := split2(part, "-")
			startMajor, startMinor, err := splitPythonVersion(start)
			if err != nil {
				return nil, err
			}
			endMajor, endMinor, err := splitPythonVersion(end)
			if err != nil {
				return nil, err
			}

			if startMajor != endMajor {
				return nil, fmt.Errorf("Invalid start and end minor versions: %d, %d", startMajor, endMajor)
			}
			for minor := startMinor; minor <= endMinor; minor++ {
				versions = append(versions, newVersion(startMajor, minor))
			}
		} else {
			versions = append(versions, part)
		}
	}
	return versions, nil
}

func newVersion(major int, minor int) string {
	return fmt.Sprintf("%d.%d", major, minor)
}

func splitPythonVersion(version string) (major int, minor int, err error) {
	version = strings.TrimSpace(version)
	majorStr, minorStr := split2(version, ".")
	major, err = strconv.Atoi(majorStr)
	if err != nil {
		return 0, 0, err
	}
	minor, err = strconv.Atoi(minorStr)
	if err != nil {
		return 0, 0, err
	}
	return major, minor, nil
}
