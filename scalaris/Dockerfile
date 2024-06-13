FROM alpine:3.19

RUN apk update && \
    apk upgrade && \
    apk --no-cache add curl jq file

# CometBFT will be looking for the genesis file in /cometbft/config/genesis.json
# (unless you change `genesis_file` in config.toml). You can put your config.toml and
# private validator file into /cometbft/config.
#
# The /cometbft/data dir is used by CometBFT to store state.
ENV CMTHOME /scalaris

# You can overwrite these before the first run to influence
# config.json and genesis.json. Additionally, you can override
# CMD to add parameters to `cometbft node`.
ENV PROXY_APP=kvstore MONIKER=dockernode CHAIN_ID=dockerchain

VOLUME /scalaris
WORKDIR /scalaris
EXPOSE 26656 26657 26660
ENTRYPOINT ["/usr/bin/start.sh"]
CMD ["node"]
STOPSIGNAL SIGTERM

COPY start.sh /usr/bin/start.sh

