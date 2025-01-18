for f in *testdata/*new; do
	ORIG="${f%%.new}"
	UPDATED="${f}"
	diff -u -w "${ORIG}" "${UPDATED}"
	mv -i "${UPDATED}" "${ORIG}"
done
