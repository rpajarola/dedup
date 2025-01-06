#!/bin/bash

for filename in "${@}"; do

  tcfile="$(basename "${filename}").json"

  if [ -f "${tcfile}" ]; then
    echo "skipping ${tcfile}"
    continue
  fi
  echo creating "${tcfile}"

  cat<<EOF>"${tcfile}"
{
  "SourceFile": "${filename}",
  "Skip": true,

  "WantCameraModel": "fixme",
  "WantCameraSerial": "fixme",
  "WantPhotoID": "fixme",
  "WantModelSerialPhotoID": "fixme",
  "WantQuality": 0
}
EOF

done
