#!/bin/sh
#
# This script should be run via curl:
#   sh -c "$(curl -fsSL https://raw.githubusercontent.com/replicate/cog/main/tools/install.sh)"
# or via wget:
#   sh -c "$(wget -qO- https://raw.githubusercontent.com/replicate/cog/main/tools/install.sh)"
# or via fetch:
#   sh -c "$(fetch -o - https://raw.githubusercontent.com/replicate/cog/main/tools/install.sh)"
#
# As an alternative, you can first download the install script and run it afterwards:
#   wget https://raw.githubusercontent.com/replicate/cog/main/tools/install.sh
#   sh install.sh
#
# You can tweak the install location by setting the INSTALL_DIR env var when running the script.
#   INSTALL_DIR=~/my/custom/install/location sh install.sh
#
# By default, cog will be installed at /usr/local/bin/cog


# This install script is based on that of ohmyzsh[1], which is licensed under the MIT License
# [1] https://github.com/ohmyzsh/ohmyzsh/blob/master/tools/install.sh
# MIT License

# Copyright (c) 2009-2022 Robby Russell and contributors (https://github.com/ohmyzsh/ohmyzsh/contributors)

# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:

# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.

# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.
set -e


command_exists() {
  command -v "$@" >/dev/null 2>&1
}

user_can_sudo() {
  # Check if sudo is installed
  command_exists sudo || return 1
  # Termux can't run sudo, so we can detect it and exit the function early.
  case "$PREFIX" in
  *com.termux*) return 1 ;;
  esac
  # The following command has 3 parts:
  #
  # 1. Run `sudo` with `-v`. Does the following:
  #    • with privilege: asks for a password immediately.
  #    • without privilege: exits with error code 1 and prints the message:
  #      Sorry, user <username> may not run sudo on <hostname>
  #
  # 2. Pass `-n` to `sudo` to tell it to not ask for a password. If the
  #    password is not required, the command will finish with exit code 0.
  #    If one is required, sudo will exit with error code 1 and print the
  #    message:
  #    sudo: a password is required
  #
  # 3. Check for the words "may not run sudo" in the output to really tell
  #    whether the user has privileges or not. For that we have to make sure
  #    to run `sudo` in the default locale (with `LANG=`) so that the message
  #    stays consistent regardless of the user's locale.
  #
  ! LANG= sudo -n -v 2>&1 | grep -q "may not run sudo"
}

check_docker() {
  if ! command_exists docker; then
  echo "Docker is not installed on your system. Please install Docker before proceeding."
    exit 1
  fi

  if ! docker run hello-world >/dev/null 2>&1; then
    echo "Docker engine is not running, or docker cannot be run without sudo. Please setup Docker so that your user has permission to run it: https://docs.docker.com/engine/install/linux-postinstall/"
    exit 1
  fi
}

setup_cog() {
  COG_LOCATION="${INSTALL_DIR}/cog"
  BINARY_URI="https://github.com/replicate/cog/releases/latest/download/cog_$(uname -s)_$(uname -m)"
  if [ -f "$COG_LOCATION" ]; then
    echo "A file already exists at $COG_LOCATION"
    echo "Do you want to delete this file and continue with this installation anyway?"
    read -p "Delete file? (y/N): " choice
    case "$choice" in 
      y|Y ) echo "Deleting existing file and continuing with installation..."; sudo rm $COG_LOCATION;;
      * ) echo "Exiting installation."; exit 1;;
    esac
  fi
  if command_exists curl; then
    sudo curl -o $COG_LOCATION -L $BINARY_URI
  elif command_exists wget; then
    sudo wget $BINARY_URI -O $COG_LOCATION
  elif command_exists fetch; then
    sudo fetch -o $COG_LOCATION $BINARY_URI
  else
    echo "One of curl, wget, or fetch must be present for this installer to work."
    exit 1
  fi
  if [ "$(cat $COG_LOCATION)" = "Not Found" ]; then
    echo "Error: Cog binary not found at ${BINARY_URI}. Check releases to see if a binary is available for your system."
    rm $COG_LOCATION
    exit 1
  fi

  sudo chmod +x $COG_LOCATION

  SHELL_NAME=$(basename "$SHELL")
  if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo "Adding $INSTALL_DIR to PATH in .$SHELL_NAME"rc
    echo "" >> ~/.$SHELL_NAME"rc"
    echo "# Created by \`cog\` install script on $(date)" >> ~/.$SHELL_NAME"rc"
    echo "export PATH=\$PATH:$INSTALL_DIR" >> ~/.$SHELL_NAME"rc"
    source ~/.$SHELL_NAME"rc"

    echo "You may need to open a new terminal window to run cog for the first time."
  fi
    
  echo
}


print_success() {
  echo "Successfully installed cog. Run \`cog login\` to configure Replicate access"
}

main() {

  # Check if macOS
  if [ "$(uname -s)" = "Darwin" ]; then
    echo "On macOS, it is recommended to install cog using Homebrew instead:"
    echo \`brew install cog\`
    echo "Do you want to continue with this installation anyway?"
    
    read -p "Continue? (y/N): " choice
    case "$choice" in 
      y|Y ) echo "Continuing with installation...";;
      * ) echo "Exiting installation."; exit 1;;
    esac
  fi

  # Set install directory
  read -p "Install location? [/usr/local/bin]: " INSTALL_DIR
  if [ ! -d "$INSTALL_DIR" ]; then
    echo "The directory $INSTALL_DIR does not exist. Please create it and re-run this script."
    # Ask user to manually create directory rather than making it for them,
    # so they don't just type in "y" again and accidentally install at ./y
    exit 1
  fi
  # Expand abbreviations in INSTALL_DIR
  INSTALL_DIR=$(cd "$INSTALL_DIR"; pwd)

  # Check if `cog` command already exists
  if command_exists cog; then
    echo "A cog command already exists on your system at the following location: $(which cog)".
    echo "The installations may interfere with one another."
    echo "Do you want to continue with this installation anyway?"
    read -p "Continue? (y/N): " choice
    case "$choice" in 
      y|Y ) echo "Continuing with installation...";;
      * ) echo "Exiting installation."; exit 1;;
    esac
  fi
  if ! user_can_sudo; then
    echo "You need sudo permissions to run this install script. Please try again as a sudoer."
    exit 1
  fi

  check_docker
  setup_cog

  if command_exists cog; then
    print_success
  else
    echo 'Error: cog not installed.'
    exit 1
  fi
}

main "$@"
