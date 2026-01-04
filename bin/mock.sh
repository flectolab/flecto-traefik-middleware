#!/bin/bash

go install go.uber.org/mock/mockgen@latest

rm -rf mocks


mockgen -destination=mocks/flecto-client/mock.go -package=mockFlectoClient github.com/flectolab/go-client HTTPClient,Client
