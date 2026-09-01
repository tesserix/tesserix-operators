ARG BUILDER_IMAGE=ghcr.io/tesserix/base-go-builder:weekly
ARG RUNTIME_IMAGE=ghcr.io/tesserix/base-distroless-static:weekly

FROM ${BUILDER_IMAGE} AS build
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p out \
    && go build -trimpath -ldflags='-s -w' -o out/devai-sandbox-sync ./cmd/sync \
    && go build -trimpath -ldflags='-s -w' -o out/devai-sandbox-operator ./cmd/operator \
    && go build -trimpath -ldflags='-s -w' -o out/zitadel-operator ./operators/zitadel/cmd/operator

FROM ${RUNTIME_IMAGE} AS sync
COPY --from=build /workspace/out/devai-sandbox-sync /devai-sandbox-sync
USER 65532:65532
ENTRYPOINT ["/devai-sandbox-sync"]

FROM ${RUNTIME_IMAGE} AS operator
COPY --from=build /workspace/out/devai-sandbox-operator /devai-sandbox-operator
USER 65532:65532
ENTRYPOINT ["/devai-sandbox-operator"]

FROM ${RUNTIME_IMAGE} AS zitadel-operator
COPY --from=build /workspace/out/zitadel-operator /zitadel-operator
USER 65532:65532
ENTRYPOINT ["/zitadel-operator"]
