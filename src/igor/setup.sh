#!/bin/bash -e

echo This script will run the following commands to set up igor. Press Ctrl-C to cancel, or hit Enter to continue:
echo useradd igor
echo cp ../../bin/igor /usr/local/bin/
echo chown igor:igor /usr/local/bin/igor
echo chmod +s /usr/local/bin/igor
echo mkdir -p $1
echo mkdir $1/igor
echo mkdir $1/pxelinux.cfg/igor
echo chown igor:igor $1/igor
echo chown igor:igor $1/pxelinux.cfg/igor
echo touch $1/igor/reservations.json
echo chown igor:igor $1/igor/reservations.json
echo cp sampleconfig.json /etc/igor.conf
echo chown igor:igor /etc/igor.conf
echo chmod 600 /etc/igor.conf
read
useradd igor
cp ../../bin/igor /usr/local/bin/
chown igor:igor /usr/local/bin/igor
chmod +s /usr/local/bin/igor
mkdir -p $1
mkdir $1/igor
mkdir $1/pxelinux.cfg/igor
chown igor:igor $1/igor
chown igor:igor $1/pxelinux.cfg/igor
touch $1/igor/reservations.json
chown igor:igor $1/igor/reservations.json
cp sampleconfig.json /etc/igor.conf
chown igor:igor /etc/igor.conf
chmod 600 /etc/igor.conf
