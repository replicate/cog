import random
import re
import string
import subprocess
import time

from packaging.version import VERSION_PATTERN

# From the SemVer spec: https://semver.org/
SEMVER_PATTERN = r"^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)(?:-(?P<prerelease>(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$"


# Used to help ensure that the cog binary reports a semver version that matches
# the PEP440 version of the embedded Python package.
#
# These are all valid pairs:
#
#   SEMVER            PEP440           NOTES
#   0.11.2            0.11.2
#   0.11.2-alpha2     0.11.2a2         prerelease counters are not checked
#   0.11.2-beta1      0.11.2b1             "          "      "   "     "
#   0.11.2-rc4        0.11.2rc4            "          "      "   "     "
#   0.11.2-dev        0.11.2rc4.dev10  dev status overrides prerelease status
#   0.11.2+gabcd      0.11.2+gabce
#
# The following are not valid pairs:
#
#   SEMVER            PEP440           NOTES
#   0.11.2            0.11.3           mismatched release versions
#   0.11.2-alpha2     0.11.2alpha2     PEP440 uses 'a' instead of 'alpha'
#   0.11.2-alpha2     0.11.2b2         mismatched prerelease status
#   0.11.2-rc4        0.11.2rc4.dev10  dev status should have overridden prerelease status
#   0.11.2+gabcd      0.11.2+gdefg     mismatched local/build metadata
#
def assert_versions_match(semver_version: str, pep440_version: str):
    semver_re = re.compile(SEMVER_PATTERN)
    pep440_re = re.compile(VERSION_PATTERN, re.VERBOSE | re.IGNORECASE)

    semver_match = semver_re.match(semver_version)
    pep440_match = pep440_re.match(pep440_version)

    assert semver_match, f"Invalid semver version: {semver_version}"
    assert pep440_match, f"Invalid PEP 440 version: {pep440_version}"

    semver_groups = semver_match.groupdict()
    pep440_groups = pep440_match.groupdict()

    semver_release = (
        f"{semver_groups['major']}.{semver_groups['minor']}.{semver_groups['patch']}"
    )

    # Check base release version
    assert (
        semver_release == pep440_groups["release"]
    ), f"Release versions do not match: {semver_release} != {pep440_groups['release']}"

    # Check prerelease status
    semver_pre = semver_groups["prerelease"]
    pep440_pre = pep440_groups["pre"] or pep440_groups["dev"]

    assert bool(semver_pre) == bool(pep440_pre), "Pre-release status does not match"

    if semver_pre:
        if semver_pre.startswith("alpha"):
            assert (
                pep440_groups["pre_l"] == "a"
            ), "Alpha pre-release status does not match"
            assert not pep440_groups[
                "dev"
            ], "Semver pre-release cannot also be a PEP440 dev build"

        if semver_pre.startswith("beta"):
            assert (
                pep440_groups["pre_l"] == "b"
            ), "Beta pre-release status does not match"
            assert not pep440_groups[
                "dev"
            ], "Semver pre-release cannot also be a PEP440 dev build"

        if semver_pre.startswith("rc"):
            assert (
                pep440_groups["pre_l"] == "rc"
            ), "Release candidate pre-release status does not match"
            assert not pep440_groups[
                "dev"
            ], "Semver pre-release cannot also be a PEP440 dev build"

        if semver_pre.startswith("dev"):
            assert pep440_groups["dev_l"] == "dev", "Dev build status does not match"

    assert (
        semver_groups["buildmetadata"] == pep440_groups["local"]
    ), f"Local/build metadata component does not match: {semver_groups['buildmetadata']} != {pep440_groups['local']}"


def random_string(length):
    return "".join(random.choice(string.ascii_lowercase) for i in range(length))


def remove_docker_image(image_name, max_attempts=5, wait_seconds=1):
    for attempt in range(max_attempts):
        try:
            subprocess.run(
                ["docker", "rmi", "-f", image_name], check=True, capture_output=True
            )
            print(f"Image {image_name} successfully removed.")
            break
        except subprocess.CalledProcessError as e:
            print(f"Attempt {attempt + 1} failed: {e.stderr.decode()}")
            time.sleep(wait_seconds)
    else:
        print(f"Failed to remove image {image_name} after {max_attempts} attempts.")
