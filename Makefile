BEAT_NAME=o365beat
BEAT_PATH=github.com/counteractive/o365beat
BEAT_GOPATH=$(firstword $(subst :, ,${GOPATH}))
SYSTEM_TESTS=false
TEST_ENVIRONMENT=false
ES_BEATS?=./vendor/github.com/elastic/beats
LIBBEAT_MAKEFILE=$(ES_BEATS)/libbeat/scripts/Makefile
GOPACKAGES=$(shell govendor list -no-status +local)
GOBUILD_FLAGS=-i -ldflags "-X $(BEAT_PATH)/vendor/github.com/elastic/beats/libbeat/version.buildTime=$(NOW) -X $(BEAT_PATH)/vendor/github.com/elastic/beats/libbeat/version.commit=$(COMMIT_ID)"
MAGE_IMPORT_PATH=${BEAT_PATH}/vendor/github.com/magefile/mage
NO_COLLECT=true

# for build purposes (doesn't fix version command in cmd/root.go):
override BEAT_VERSION=1.5.1
override BEAT_VENDOR=Counteractive

# Path to the libbeat Makefile
-include $(LIBBEAT_MAKEFILE)

# Initial beat setup
.PHONY: setup
setup: pre-setup

pre-setup: copy-vendor
	$(MAKE) -f $(LIBBEAT_MAKEFILE) mage ES_BEATS=$(ES_BEATS)
	$(MAKE) -f $(LIBBEAT_MAKEFILE) update BEAT_NAME=$(BEAT_NAME) ES_BEATS=$(ES_BEATS) NO_COLLECT=$(NO_COLLECT)

# Copy beats (pinned to version 7.5.1) into vendor directory
.PHONY: copy-vendor
copy-vendor:
	mkdir -p vendor/github.com/elastic
	git clone -b 'v7.5.1' --depth 1 https://github.com/elastic/beats.git vendor/github.com/elastic/beats
	rm -rf vendor/github.com/elastic/beats/.git vendor/github.com/elastic/beats/x-pack
	# copy whatever version of mage beats is using into vendor directory
	mkdir -p vendor/github.com/magefile
	cp -R vendor/github.com/elastic/beats/vendor/github.com/magefile/mage vendor/github.com/magefile

MAGE_VERSION     ?= v1.8.0
MAGE_PRESENT     := $(shell mage --version 2> /dev/null | grep $(MAGE_VERSION))
MAGE_IMPORT_PATH ?= github.com/elastic/beats/vendor/github.com/magefile/mage
export MAGE_IMPORT_PATH

.PHONY: mage
mage:
ifndef MAGE_PRESENT
	@echo Installing mage $(MAGE_VERSION) from vendor dir.
	@go install -ldflags="-X $(MAGE_IMPORT_PATH)/mage.gitTag=$(MAGE_VERSION)" ${MAGE_IMPORT_PATH}
	@-mage -clean
endif
	@true

# DONE: figure out how to set the version for the custom beat (BEAT_VERSION?)
# TODO: check for gcc and exit if it's not installed
# TODO: check for virtualenv and exit if it's not installed
# TODO: check for docker and check to be sure the user is in the docker group
# $ sudo usermod -aG docker $USER
# note: the snap version via the installer for 18.04 appears to be unsupported
# and doesn't work well.  use the instructions for CE at https://docs.docker.com/install/linux/docker-ce/ubuntu/
