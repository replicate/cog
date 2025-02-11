pushd test-integration/test_integration/fixtures/fast-build
dd if=/dev/urandom of=output.dat bs=1G count=1
sha256sum output.dat
../../../../cog push --debug --x-fast
rm output.dat
popd

