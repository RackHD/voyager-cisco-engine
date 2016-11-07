FROM ubuntu:16.04

ADD bin/voyager-cisco-engine voyager-cisco-engine
ADD templates/ templates/
ADD workflows/ workflows/

COPY workflows/ workflows
COPY templates/ templates

CMD ./voyager-cisco-engine
