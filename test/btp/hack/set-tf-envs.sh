#!/bin/bash

echo "$BTP_ENV" > /tmp/.env
export $(cat /tmp/.env | xargs -L 1)
rm /tmp/.env

