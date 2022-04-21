# What?

This repository has test suites designed to run across different builds of clusters, the intent is to verify that
upgrading from one release to the next will not introduce breaking changes in difficult to test areas like mirrors etc

It stands up a Docker Compose environment consisting of 2 clusters (c1, c2) with 3 servers in each cluster, all with
JetStream enabled.

# Design?

We have various workflows that first start a cluster on an older version, run the full suite and then upgrades the 
cluster to a new version where only the `Validate` steps are run.

Each test suite has a number of scenarios and each has `Create` and `Validate` sections. The `Create` ones should
call `Skip()` when `VALIDATE_ONLY` environment variable is set.

See `.github/workflows` for the various scenarios.

# Suites?

| Suite                          | Description                                   |
|--------------------------------|-----------------------------------------------|
| `streams/basic_mirror_test.go` | Creates a R3 streams in C2 and a mirror in C1 |
| `streams/relocate_test.go`     | Creates a R3 in C2 then relocates to C1       |

# Status?

It's a work in progress with minimal coverage aimed at surfacing and testing problems found in Server 2.8.0 release, 
this repository would have avoided those problems had it existed during the development cycle.
