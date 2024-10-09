#!/bin/bash

echo "$BTP_ENV" > /tmp/.env
export $(cat /tmp/.env | xargs)
cat /tmp/.env | wc -l
rm /tmp/.env
