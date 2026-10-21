FROM gcc:14-bookworm AS build
WORKDIR /src
COPY chaos/slowdisk.c ./
RUN gcc -shared -fPIC -O2 -Wall -Wextra -Werror slowdisk.c -ldl -o /libslowdisk.so
FROM postgres:17
COPY --from=build /libslowdisk.so /opt/libslowdisk.so
RUN mkdir -p /faults && chmod 755 /faults
ENV LD_PRELOAD=/opt/libslowdisk.so
