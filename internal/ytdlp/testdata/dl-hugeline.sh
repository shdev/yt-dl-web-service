#!/bin/sh
head -c 70000 /dev/zero | tr '\0' 'x'
printf '\n'
exit 0
