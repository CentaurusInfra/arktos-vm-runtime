# Pixiecore, with embedded iPXE

This is a version of the Pixiecore tool that embeds iPXE binaries for
all of Pixiecore's supported architectures/firmwares, so you don't
have to handle building and deploying them yourself.

Due to iPXE's license, the result of embedding iPXE builds in the
Pixiecore binary makes the overall binary fall under the terms of the
GPLv2. See the LICENSE file in this directory for that license.

If you want an Apache-licensed build, check the "pixiecore-apache2"
directory one level up for a build that doesn't embed iPXE (you have
to supply iPXE binaries at runtime).
