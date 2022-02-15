# Change the paths to be relative when copying the readme into the docs,
# so the links are correct (they need to be relative to the docs folder, not the root dir).
sed  's/docs\///g' README.md > ./docs/README.md
cp CONTRIBUTING.md ./docs/


