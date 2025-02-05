for f in *testdata/*new; do
	ORIG="${f%%.new}"
	UPDATED="${f}"
	if diff -q "${ORIG}" "${UPDATED}"; then
		rm "${UPDATED}"
		continue
	fi
	diff -u -w "${ORIG}" "${UPDATED}"
	mv -i "${UPDATED}" "${ORIG}"
done
