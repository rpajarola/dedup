#!/bin/bash

for filename in "${@}"; do
  tcfile=$(basename "${filename}").textproto
  tcfile="${tcfile// /_}"

  if [ -f "${tcfile}" ]; then
    continue
  fi
  echo creating "${tcfile}"

  cat<<EOF>"${tcfile}"
source_file: "${filename}"

want_fingerprint {
	want_kind: "EXIFModelSerialPhotoID"
	want_hash: "fixme"
	want_quality: 0
}
want_fingerprint {
	want_kind: "XMPDocumentID"
	want_hash: "fixme"
	want_quality: 0
}

exif {
	want_camera_model: "fixme"
	want_camera_serial: "fixme"
	want_photo_id: "fixme"
}

xmp {
	want_document_id: "fixme"
}
EOF

done
