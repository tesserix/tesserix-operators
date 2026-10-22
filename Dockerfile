ARG BUILDER_IMAGE=ghcr.io/tesserix/base-go-builder:weekly@sha256:f9214aed0fe09af409811bd4581bbd4af586d1a72bb667a67d03a446857dd47c
ARG RUNTIME_IMAGE=ghcr.io/tesserix/base-distroless-static:weekly@sha256:7c885c37cb2f61400256bef39b9221b48a4860f8080101765da1c262b994cdf1

FROM ${BUILDER_IMAGE} AS build
ARG GO_VERSION=1.27.2
ENV GOTOOLCHAIN=go${GO_VERSION}
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p out \
    && go build -trimpath -ldflags='-s -w' -o out/devai-sandbox-sync ./cmd/sync \
    && go build -trimpath -ldflags='-s -w' -o out/devai-sandbox-operator ./cmd/operator \
    && go build -trimpath -ldflags='-s -w' -o out/zitadel-operator ./operators/zitadel/cmd/operator \
    && go build -trimpath -ldflags='-s -w' -o out/analytics-onboarding-operator ./operators/openpanel/cmd/operator \
    && go build -trimpath -ldflags='-s -w' -o out/evals-onboarding-operator ./operators/evals/cmd/operator

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

FROM ${RUNTIME_IMAGE} AS analytics-onboarding-operator
COPY --from=build /workspace/out/analytics-onboarding-operator /analytics-onboarding-operator
USER 65532:65532
ENTRYPOINT ["/analytics-onboarding-operator"]

FROM ${RUNTIME_IMAGE} AS evals-onboarding-operator
COPY --from=build /workspace/out/evals-onboarding-operator /evals-onboarding-operator
USER 65532:65532
ENTRYPOINT ["/evals-onboarding-operator"]

FROM postgres:14-alpine@sha256:727876d274666da0b92a445390ba093c84b8e9f8343e1c53cd4e9a7ab2d85310 AS openpanel-project-init
RUN apk upgrade --no-cache \
    && apk add --no-cache argon2 nodejs kubectl \
    && rm -f /usr/local/bin/gosu
USER 65532:65532
