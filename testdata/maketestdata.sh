#!/bin/bash

for oldfile in "${@}"; do
	echo "${oldfile}"
        exiftool "${oldfile}" -Model -Make -SerialNumber -ShutterCount
        read -p "new test file (without .jpg)\n> " newfile
        cp 1x1#000000.jpg "${newfile}.jpg"
	exiftool -TagsFromFile "${oldfile}" -all:all "${newfile}.jpg"
	rm "${newfile}.jpg_original"
done
