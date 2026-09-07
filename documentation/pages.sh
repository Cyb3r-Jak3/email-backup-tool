#!/bin/bash
pip install -r documentation/requirements.txt
asdf plugin add task https://github.com/particledecay/asdf-task.git
asdf install task latest
asdf global task latest
task docs
task generate-schema
cp ../config.schema.json documentation/site/