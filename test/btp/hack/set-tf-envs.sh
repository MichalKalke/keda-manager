#!/bin/bash

echo "$BTP_ENV" > /tmp/.env
cat /tmp/.env | wc -l
export $(</tmp/.env)
rm /tmp/.env

