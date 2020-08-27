FROM BASEIMAGE
RUN apk --no-cache add ca-certificates bash

ARG ARCH
ARG TINI_VERSION

ADD agent /usr/local/bin/
EXPOSE 8080
USER 1001
ENTRYPOINT ["agent"]