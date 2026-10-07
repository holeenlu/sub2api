"""Classify numeric SemVer refs for branded Docker and GitHub releases."""
import argparse
import re

NUMBER = r"(?:0|[1-9][0-9]*)"
IDENTIFIER = rf"(?:{NUMBER}|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
SEMVER = re.compile(
    rf"{NUMBER}\.{NUMBER}\.{NUMBER}(?:\.{NUMBER})?"
    rf"(?:-(?P<prerelease>{IDENTIFIER}(?:\.{IDENTIFIER})*))?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
)


def release_metadata(ref):
    if not ref.startswith("refs/tags/"):
        return {"version": "", "image_tag": "", "prerelease": "false", "stable": "false"}
    version = ref.removeprefix("refs/tags/")
    match = SEMVER.fullmatch(version)
    if match is None:
        raise ValueError("release tag must be numeric SemVer, for example 1.0.0 or 1.0.0-rc.1")
    prerelease = match.group("prerelease") is not None
    return {
        "version": version,
        "image_tag": version.replace("+", "_"),
        "prerelease": str(prerelease).lower(),
        "stable": str(not prerelease).lower(),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ref")
    args = parser.parse_args()
    try:
        metadata = release_metadata(args.ref)
    except ValueError as error:
        parser.error(str(error))
    for key, value in metadata.items():
        print(f"{key}={value}")


if __name__ == "__main__":
    main()
