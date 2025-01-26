#!/bin/bash

git annex init || echo "git annex not initialized"

urlbase=https://cave.servium.ch/github/dedup
git annex list | while read where fname; do
  case "${where}:${fname}" in
  [X_]*:*testdata*)
    echo "${fname}"
    git annex addurl --file "${fname}" "${urlbase}/${fname}"
    ;;
  esac
done
git annex sync

git annex list | while read where fname; do
  case "${where}:${fname}" in
  [X_]*:*testdata*)
    echo "${fname}"
    git annex get "${fname}"
    ;;
  esac
done
