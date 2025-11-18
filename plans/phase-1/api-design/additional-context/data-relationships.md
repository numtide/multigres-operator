---
title: MultigresCluster Canonical Data Sources and Relationships
state: ready
---

## Summary
> This document was provided by Sugu to explain how database and shard schema is stored in a postgres database and the problem of having two sources of truth for this databases definition: Kubernetes Cluster and Multigres.


Multigres is going to have metadata in multiple places and in some cases, they have to be duplicated. If so, we have define which one is the canonical source of truth. We will also define the relationships between the different data sources.

## List of Data Sources

In a Kubernetes cluster the following data sources are used:

- Kubernetes CRD templates: This contains all the CRDs for the Multigres Operator. This is static. Example: `MultigreCluster` type.
- Kubernetes CRs: These are Kubernetes meta-objects that are instances of the CRDs. Kubernetes lets the operator manage their lifecycle. Example: `MultigreCluster` objects.
- Kubernetes objects: These are real objects like pods and services.

For simplicity, we'll unify all of the above data sources as one storage type: Kubernetes Store.

In Multigres, we have the following data sources:

- Global Toposerver: This stores the name of each MultigresCluster. Under each, it stores the per-cell metadata:
  - Cell Name
  - Local Toposerver Address

- Local Toposerver: Components running within a cell register themselves in this server. Components can discover each other using this server.

- Default TableGroup metadata: Simply put, this is a hidden area within the default database of the first Postgres server created for the MultigresCluster. In Multigres parlance, this first database is the only shard of the "default" tablegroup, which is inside the Multigres default database. Just like every Postgres server creates a default database, Multigres creates a logical default database that contains this unsharded tablegroup with a single shard. Essentially, `Multigres->Default Database->Default TableGroup->Only Shard` is the same as `Postgres->Default Database`. This metadata area contains:
  - A list of databases.
  - For each database, a list of tablegroups, of which the default is always present.
  - For each tablegroup, a list of shards. For the default tablegroup, it's always a single shard, which is the current database.

| Postgres | Physical Multigres |
|----------|---------------------|
| Server | Multigres Cluster |
| Database | Multigres Database->TableGroups->Shards (physical PG servers) |
| Default Database | Multigres Default Database->Default TableGroup->Only Shard |
| create table t | t is created in: Multigres Default Database->Default TableGroup->Only Shard |
| (new syntax) | Create tablegroup tg1 for db1 |
| create table t1 in tg1 (new syntax) | Table t1 in all shards of tg1 |

In other words, a multigres cluster would have the following metadata:
- default database
  - default tablegroup
    - only shard
- db1
  - default tablegroup
    - only shard
  - tg1
    - shard "0-8"
    - shard "8-inf"

In this structure, we have a chicken-and-egg problem because this information also exists in the Kubernetes Store under the `MultigresCluster` custom resource.

## Life of a cluster

### Cluster initialization

In the beginning, we have the Multigres Operator (MGO) running. In Kubernetes, CRD templates are already registered.

MGO receives a command to create a new MultigresCluster. Let us assume:

- Cluster name cluster1
- Single cell c1
- Single components in c1: MultiPooler, MultiGateway, and MultiOrch

MGO Actions:

- MGO creates (or reuses) a Global Toposerver. Root path: `/multigres`.
- Creates a new entry: `/multigres/cluster1/global`
- Under the above entry, it creates the following subentries:
  - `/multigres/cluster1/global/cell1`
  - `/multigres/cluster1/global/cell1/local-toposerver`: This contains the address of the local toposerver.
- MGO creates (or reuses) a Local Toposerver. Root path: `/multigres/cluster1/cell1`. No subentries at this point.
- MGO launches the pods and services for MultiPooler, MultiGateway, and MultiOrch.

Multigres Actions:

- MultiPooler comes up and registers itself with the local toposerver under `/multigres/cluster1/cell1/`.
- MultiPooler initializes an empty postgres database. In this database, it creates a hidden area for the multigres metadata. In it, it adds a default tablegroup, and a single shard, which represents the current database.
- MultiOrch discovers the MultiPooler, notices that it's a single instance situation, appoints the MultiPooler as Primary, and requests it to accept read-write traffic.
- MultiGateway discovers MultiPooler. It's for the default tablegroup. It fetches the tablegroup metadata from MultiPooler, and discovers only the default tablegroup. It sets itself up to serve traffic for this tablegroup.

Users:

- Connect to MultiGateway (with no database name). This is implicitly resolved to the default tablegroup. They can create tables and use this database just like the default database of Postgres.

#### What is the problem

The default tablegroup info is present in two places:
- It's (implicitly) present in the Multigres Cluster definition.
- It's present in the postgres database under the hidden area created by MultiPooler.

One may argue that the Multigres Cluster definition is the canonical source of truth. However, this will put the burden of maintaining the database metadata on the MultiPooler, which is not ideal. Also, this increases the surface area between MGO and Multigres.

The alternative is to have the database be the source of truth, and MGO drive off of that. We'll analyze options arising out of this approach below.

### Create Database

User issues `CREATE DATABASE db1;`