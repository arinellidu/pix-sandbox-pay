# The recording environment for docs/demo.tape.
#
# vhs ships ttyd and ffmpeg but not the tools the demo actually types, and the
# demo has to type the same curl the README does — a recording of some other
# command is a recording of some other product.
#
#   docker build -f docs/demo.Dockerfile -t pix-sandbox-vhs .
#   docker run --rm -v "$PWD:/vhs" pix-sandbox-vhs docs/demo.tape
FROM ghcr.io/charmbracelet/vhs:latest

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl jq \
    && rm -rf /var/lib/apt/lists/*
