# FAQ

## Dependencies

## FFMPEG

This requires ffmpeg 7.x

### Ubuntu

```
  sudo add-apt-repository ppa:ubuntuhandbook1/ffmpeg7
  sudo apt install libavcodec-dev libavdevice-dev libavfilter-dev libavformat-dev libswresample-dev libswscale-dev libavutil-dev
```

### MacOS X
```
  brew install ffmpeg
```

## Tests

### Fetch testdata

Large testdata is stored in git annex.

Run `get_testdata.sh` to fetch files from web.

Run `git annex sync` to sync (existing) testdata.

### Add  testdata

```
WEBDAVE_USERNAME=<user> WEBDAV_PASSWORD=<password> git annex enableremote cave.servium.ch
git annex add large_testdata/new_file
git annex copy large_testdata/new_file --to cave.servium.ch
```

### Unstable tests

Decoding of video is fickly, and some of the hashes need fixing to be
stable between platforms. (TODO)

The canonical platform for tests is Ubuntu on x64.
