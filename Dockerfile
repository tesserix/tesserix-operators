ARG BUILDER_IMAGE=ghcr.io/tesserix/base-go-builder:weekly@sha256:a92808d3b0f93b9b33c336cc56abba06688b6f004b540e0f62ea60cd695a0a2e
ARG RUNTIME_IMAGE=ghcr.io/tesserix/base-distroless-static:weekly@sha256:2c2132b20f17f2a2cdc631704b9cbfbb12746520a4646fc8514b18d7a12c3280

FROM ${BUILDER_IMAGE} AS build
ARG GO_VERSION=1.27.4
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

FROM postgres:14-alpine@sha256:50fce0c9c54c43d8b02ce16cb9daff75eb94c09c4ee3d41c157852ebd9700c80 AS openpanel-project-init
# libxml2 floor: CVE-2026-86144 is fixed in 2.13.9-r3. Naming it changes this
# layer, so a cached upgrade cannot ship the vulnerable build, and the build
# fails rather than continuing if the fixed package is unavailable.
RUN apk upgrade --no-cache \
    && apk add --no-cache argon2 nodejs kubectl 'libxml2>=2.13.9-r3' \
    && rm -f /usr/local/bin/gosu
USER 65532:65532
