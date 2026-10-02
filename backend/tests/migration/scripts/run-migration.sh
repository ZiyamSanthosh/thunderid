#!/bin/sh

set -eu

./thunderid -serverHome /opt/thunderid migration initialize
./thunderid -serverHome /opt/thunderid migration check
./thunderid -serverHome /opt/thunderid migration apply
./thunderid -serverHome /opt/thunderid migration check
./thunderid -serverHome /opt/thunderid migration apply
