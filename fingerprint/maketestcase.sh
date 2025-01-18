#!/bin/bash

for filename in "${@}"; do
  tcfile="${filename}.textproto"
  if [ -f "${tcfile}" ]; then
    continue
  fi
  sourcefile=$(basename "${filename}")
  echo creating "${tcfile}"

  cat<<EOF>"${tcfile}"
source_file: "${sourcefile}"
EOF

done
