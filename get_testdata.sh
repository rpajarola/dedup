#!/bin/bash

git annex init || echo "git annex not initialized"
git annex pull

urlbase=https://cave.servium.ch/github/dedup
git annex list | while read where fname; do
  case "${where}:${fname}" in
  ??_*:*testdata*)
    echo "${fname}"
    git annex addurl --file "${fname}" "${urlbase}/${fname}"
    ;;
  esac
done
