# Meilisearch Technology Brief

**Purpose:** Evaluate Meilisearch as a search backend for hive-server, a Go API server providing Tool abstractions and Memory systems for LLM agents.

**Date:** 2026-03-09

---

## Table of Contents

1. [What is Meilisearch?](#1-what-is-meilisearch)
2. [How the Search Engine Works](#2-how-the-search-engine-works)
3. [REST API](#3-rest-api)
4. [Go Client SDK](#4-go-client-sdk-meilisearch-go)
5. [Indexes and Search Settings](#5-indexes-and-search-settings)
6. [Key Features](#6-key-features)
7. [Deployment](#7-deployment)
8. [Comparison to Alternatives](#8-comparison-to-alternatives)
9. [Limitations](#9-limitations)
10. [Go Server Integration Patterns](#10-go-server-integration-patterns)
11. [Multi-Tenancy](#11-multi-tenancy)
12. [Document Storage vs Pure Search](#12-document-storage-vs-pure-search)
13. [Relevance to hive-server](#13-relevance-to-hive-server)

---

## 1. What is Meilisearch?

Meilisearch is an open-source, full-text search engine written in Rust. It exposes a RESTful HTTP API and is designed for instant, typo-tolerant, search-as-you-type experiences. It returns results in under 50 milliseconds.

**Core problem it solves:** Providing fast, relevant, developer-friendly search without the operational complexity of Elasticsearch or the cost/vendor-lock-in of Algolia. It is positioned as a "plug-and-play" search engine that can be deployed in minutes rather than months.

**Licensing (as of 2025):** Dual license model:

- **Community Edition (CE):** MIT license, fully open source, free for commercial use.
- **Enterprise Edition (EE):** Commercial license with features like sharding and horizontal scaling.

**Key characteristics:**

- Written in Rust for performance and safety
- Single-binary deployment (no JVM, no cluster required for basic use)
- Uses LMDB (Lightning Memory-Mapped Database) for on-disk storage
- Memory-maps data files for fast access without loading everything into RAM
- Asynchronous indexing (writes return task IDs, indexing happens in background)
- Synchronous reads (search queries are fast, real-time)

---

## 2. How the Search Engine Works

### 2.1 Indexing

When documents are added, Meilisearch:

1. **Tokenizes** text into individual terms
2. **Normalizes** tokens (lowercasing, Unicode normalization, language-specific rules)
3. Builds an **inverted index** mapping tokens to document IDs and positions within documents
4. Stores data in LMDB with memory-mapped files

Indexing is asynchronous. When you POST documents, you receive a `taskUID` immediately. The actual indexing happens in the background. You can poll task status or use `WaitForTask()` in the SDK.

**Supported formats for document upload:** JSON, NDJSON, CSV.

### 2.2 Ranking Algorithm (Bucket Sort)

Meilisearch uses a **bucket sort** algorithm rather than a scoring function. Documents are sorted into buckets by the first ranking rule. Within each bucket, the second rule acts as a tiebreaker, and so on.

**Default ranking rules (in order):**

| Rule          | What it does                                                                                        |
| ------------- | --------------------------------------------------------------------------------------------------- |
| **words**     | Documents containing more matched query terms rank higher                                           |
| **typo**      | Documents with fewer typos in matched terms rank higher                                             |
| **proximity** | Documents where matched terms are closer together rank higher                                       |
| **attribute** | Documents matching in more important attributes rank higher (based on `searchableAttributes` order) |
| **sort**      | User-defined sort order from the `sort` query parameter                                             |
| **exactness** | Documents with exact term matches (not prefix) rank higher                                          |

**Important:** The `words` rule always takes implicit highest priority even if reordered or removed.

You can reorder these rules and insert custom ranking rules (e.g., `release_date:desc`) at any position to influence relevancy.

### 2.3 Typo Tolerance

Meilisearch uses a **prefix Levenshtein automaton** built on Finite State Transducers (FSTs) to find matches with typos efficiently.

- **1 typo** allowed for words with 5+ characters
- **2 typos** allowed for words with 9+ characters
- Maximum of 2 typos per query term
- These thresholds are configurable per index
- Typo tolerance can be disabled entirely or for specific attributes

### 2.4 Hybrid / AI-Powered Search

Meilisearch supports **hybrid search** combining keyword-based full-text search with vector/semantic search:

- Configure **embedders** (OpenAI, HuggingFace, REST endpoints, or local models)
- Set a `semanticRatio` (0.0 = pure keyword, 1.0 = pure semantic)
- Auto-generate embeddings on document indexing
- Supply custom embedding vectors per document
- Supports multimodal search (images + text)

---

## 3. REST API

Meilisearch exposes its entire functionality through a RESTful HTTP API on port 7700 (default).

### 3.1 Core Endpoints

| Category         | Endpoints                           | Methods                |
| ---------------- | ----------------------------------- | ---------------------- |
| **Search**       | `/indexes/{uid}/search`             | POST, GET              |
| **Multi-Search** | `/multi-search`                     | POST                   |
| **Documents**    | `/indexes/{uid}/documents`          | GET, POST, PUT, DELETE |
| **Indexes**      | `/indexes`                          | GET, POST              |
|                  | `/indexes/{uid}`                    | GET, PATCH, DELETE     |
| **Settings**     | `/indexes/{uid}/settings`           | GET, PATCH, DELETE     |
|                  | `/indexes/{uid}/settings/{setting}` | GET, PUT, DELETE       |
| **Tasks**        | `/tasks`                            | GET                    |
|                  | `/tasks/{uid}`                      | GET                    |
| **Keys**         | `/keys`                             | GET, POST              |
|                  | `/keys/{uid}`                       | GET, PATCH, DELETE     |
| **Health**       | `/health`                           | GET                    |
| **Stats**        | `/stats`                            | GET                    |
| **Dumps**        | `/dumps`                            | POST                   |
| **Snapshots**    | `/snapshots`                        | POST                   |
| **Chat** (new)   | `/chat/completions`                 | POST                   |

### 3.2 Authentication

Meilisearch uses **API keys** with a master key system:

- **Master key:** Set via `MEILI_MASTER_KEY` env var. Required in production mode. Grants full access.
- **API keys:** Created via the `/keys` endpoint. Scoped by actions, indexes, and expiration dates.
- Keys are passed in the `Authorization: Bearer <key>` header.

### 3.3 Search Request Parameters

```json
POST /indexes/movies/search
{
  "q": "search terms",
  "filter": "genre = 'action' AND year > 2020",
  "sort": ["year:desc"],
  "facets": ["genre", "year"],
  "limit": 20,
  "offset": 0,
  "attributesToRetrieve": ["id", "title", "genre"],
  "attributesToHighlight": ["title"],
  "attributesToCrop": ["overview"],
  "cropLength": 50,
  "matchingStrategy": "last",
  "showRankingScore": true,
  "hybrid": {
    "embedder": "default",
    "semanticRatio": 0.5
  }
}
```

### 3.4 Search Response

```json
{
  "hits": [
    {
      "id": 1,
      "title": "Example Movie",
      "_rankingScore": 0.95,
      "_formatted": {
        "title": "<em>Example</em> Movie"
      }
    }
  ],
  "query": "search terms",
  "processingTimeMs": 12,
  "estimatedTotalHits": 42,
  "facetDistribution": {
    "genre": { "action": 15, "comedy": 27 }
  },
  "facetStats": {
    "year": { "min": 1990, "max": 2025 }
  }
}
```

### 3.5 Asynchronous Write Model

All write operations (document adds, settings updates, index creation) are **asynchronous**:

1. Client sends request
2. Meilisearch returns `{ "taskUid": 123, "status": "enqueued" }`
3. Client polls `GET /tasks/123` or uses SDK's `WaitForTask()`
4. Task progresses: `enqueued` -> `processing` -> `succeeded` / `failed`

---

## 4. Go Client SDK (meilisearch-go)

**Repository:** `github.com/meilisearch/meilisearch-go`
**Minimum Go version:** 1.21
**Compatibility:** Meilisearch v1.x
**License:** MIT

### 4.1 Installation

```bash
go get github.com/meilisearch/meilisearch-go
```

### 4.2 Client Initialization

```go
import meilisearch "github.com/meilisearch/meilisearch-go"

// Basic
client := meilisearch.New("http://localhost:7700",
    meilisearch.WithAPIKey("your-master-key"))

// With options
client := meilisearch.New("http://localhost:7700",
    meilisearch.WithAPIKey("your-key"),
    meilisearch.WithContentEncoding(meilisearch.GzipEncoding, meilisearch.BestCompression),
    meilisearch.WithCustomRetries([]int{502, 503}, 20),
)

// Custom JSON marshaler for performance
import "github.com/bytedance/sonic"

client := meilisearch.New("http://localhost:7700",
    meilisearch.WithAPIKey("your-key"),
    meilisearch.WithCustomJsonMarshaler(sonic.Marshal),
    meilisearch.WithCustomJsonUnmarshaler(sonic.Unmarshal),
)
```

### 4.3 Index and Document Operations

```go
// Create index
taskInfo, err := client.CreateIndex(&meilisearch.IndexConfig{
    Uid:        "memories",
    PrimaryKey: "id",
})

// Add documents
docs := []map[string]interface{}{
    {"id": "mem-001", "content": "User prefers dark mode", "agent_id": "agent-1"},
    {"id": "mem-002", "content": "Project deadline is March 15", "agent_id": "agent-2"},
}
taskInfo, err := client.Index("memories").AddDocuments(docs)

// Wait for indexing to complete
task, err := client.WaitForTask(taskInfo.TaskUID)

// Get a document
var doc map[string]interface{}
err := client.Index("memories").GetDocument("mem-001", nil, &doc)

// Update documents (partial update, merges with existing)
updates := []map[string]interface{}{
    {"id": "mem-001", "content": "User prefers dark mode with blue accent"},
}
taskInfo, err := client.Index("memories").UpdateDocuments(updates)

// Delete documents
taskInfo, err := client.Index("memories").DeleteDocument("mem-001")
taskInfo, err := client.Index("memories").DeleteDocumentsByFilter("agent_id = agent-1")
```

### 4.4 Search

```go
// Basic search
result, err := client.Index("memories").Search("dark mode", &meilisearch.SearchRequest{
    Limit: 10,
})

// Filtered search
result, err := client.Index("memories").Search("deadline", &meilisearch.SearchRequest{
    Filter: "agent_id = 'agent-2' AND type = 'task'",
    Sort:   []string{"created_at:desc"},
    Limit:  20,
})

// Faceted search
result, err := client.Index("memories").Search("preferences", &meilisearch.SearchRequest{
    Facets: []string{"agent_id", "type", "priority"},
})

// Multi-index search
results, err := client.MultiSearch(&meilisearch.MultiSearchRequest{
    Queries: []meilisearch.SearchRequest{
        {IndexUID: "memories", Query: "deadline"},
        {IndexUID: "tools", Query: "deadline"},
    },
})
```

### 4.5 Settings Configuration

```go
settings := &meilisearch.Settings{
    SearchableAttributes: []string{"content", "title", "tags"},
    FilterableAttributes: []string{"agent_id", "type", "priority", "created_at"},
    SortableAttributes:   []string{"created_at", "priority"},
    DisplayedAttributes:  []string{"id", "content", "title", "agent_id", "type", "created_at"},
    StopWords:            []string{"the", "a", "an", "is", "are"},
    Synonyms: map[string][]string{
        "task":    {"todo", "action item"},
        "memory":  {"recollection", "note"},
    },
    RankingRules: []string{
        "words", "typo", "proximity", "attribute", "sort", "exactness",
    },
    TypoTolerance: &meilisearch.TypoTolerance{
        Enabled: true,
        MinWordSizeForTypos: &meilisearch.MinWordSizeForTypos{
            OneTypo:  5,
            TwoTypos: 9,
        },
    },
    Pagination: &meilisearch.Pagination{
        MaxTotalHits: 5000,
    },
}

taskInfo, err := client.Index("memories").UpdateSettings(settings)
_, err = client.WaitForTask(taskInfo.TaskUID)
```

### 4.6 Testing with Mocks

The SDK provides generated mocks via testify/mock:

```go
import "github.com/meilisearch/meilisearch-go/mocks"

mockClient := mocks.NewMockmeilisearchServiceManager(t)
mockIndex := mocks.NewMockIndexManager(t)

mockClient.On("Index", "memories").Return(mockIndex)
mockIndex.On("Search", "test query", mock.Anything).Return(
    &meilisearch.SearchResponse{
        Hits: []interface{}{
            map[string]interface{}{"id": "1", "content": "test result"},
        },
    }, nil,
)
```

### 4.7 Known Issue

There is a known mTLS interoperability issue (as of September 2025) where Meilisearch's rustls backend (using aws-lc-rs) is incompatible with Go's `crypto/tls`. This only affects mTLS configurations, not standard TLS.

---

## 5. Indexes and Search Settings

### 5.1 Index Concepts

An **index** is the top-level container in Meilisearch, analogous to a database table. Each index:

- Has a unique `uid` (string identifier, e.g., `"memories"`, `"tools"`)
- Has a `primaryKey` attribute that uniquely identifies each document
- Has its own independent settings (searchable fields, filters, ranking rules, etc.)
- Can be swapped atomically with another index (zero-downtime reindexing)

### 5.2 Configurable Settings Per Index

| Setting                | Purpose                                                    |
| ---------------------- | ---------------------------------------------------------- |
| `searchableAttributes` | Fields searched for query matches, ordered by importance   |
| `filterableAttributes` | Fields that can appear in `filter` expressions             |
| `sortableAttributes`   | Fields that can appear in `sort` parameters                |
| `displayedAttributes`  | Fields returned in search results                          |
| `rankingRules`         | Ordered list of ranking rules                              |
| `stopWords`            | Words ignored during search                                |
| `synonyms`             | Map of equivalent terms                                    |
| `distinctAttribute`    | Field used to deduplicate results                          |
| `typoTolerance`        | Typo tolerance configuration                               |
| `pagination`           | Max total hits configuration                               |
| `faceting`             | Max values per facet                                       |
| `separatorTokens`      | Custom separator characters                                |
| `nonSeparatorTokens`   | Characters that should not be treated as separators        |
| `dictionary`           | Custom words for tokenization                              |
| `proximityPrecision`   | Precision of proximity ranking (`byWord` or `byAttribute`) |
| `searchCutoffMs`       | Maximum search time before timeout                         |
| `embedders`            | AI/vector search embedder configuration                    |
| `localizedAttributes`  | Per-field language settings                                |

### 5.3 Setting Searchable Attributes

Order matters. First attribute is most important for the `attribute` ranking rule:

```go
// Title matches rank higher than description matches
index.UpdateSearchableAttributes(&[]string{
    "title",       // highest priority
    "content",     // medium priority
    "tags",        // lower priority
    "metadata",    // lowest priority
})
```

### 5.4 Primary Key

Every document must have a primary key field. Meilisearch will auto-detect it if it finds a field named `id` or ending in `id`. You can also set it explicitly:

```go
client.CreateIndex(&meilisearch.IndexConfig{
    Uid:        "memories",
    PrimaryKey: "memory_id",
})
```

Primary key values are limited to **511 bytes**.

---

## 6. Key Features

### 6.1 Filtering

Filter expressions support SQL-like syntax:

```
genre = 'action'
year > 2020
genre IN ['action', 'comedy']
genre = 'action' AND year > 2020
genre = 'action' OR genre = 'comedy'
NOT genre = 'horror'
_geoRadius(lat, lng, distance_in_meters)
_geoBoundingBox([lat1, lng1], [lat2, lng2])
```

Fields must be declared in `filterableAttributes` before use.

### 6.2 Faceting

Request facet distributions in search results:

```json
{ "facets": ["genre", "year", "rating"] }
```

Response includes:

- `facetDistribution`: count of documents per facet value
- `facetStats`: min/max for numeric facets

Limitation: synonyms do not apply to facet values.

### 6.3 Sorting

Declare sortable attributes, then sort at query time:

```json
{ "sort": ["year:desc", "title:asc"] }
```

Also supports geo sorting: `"_geoPoint(lat, lng):asc"`.

### 6.4 Synonyms

Define bidirectional or unidirectional synonym mappings:

```go
index.UpdateSynonyms(&map[string][]string{
    "phone":    {"cellphone", "mobile"},
    "laptop":   {"notebook", "computer"},
})
```

Synonyms apply to search queries but NOT to filters or facets.

### 6.5 Geosearch

Filter and sort by geographic coordinates:

- Documents must have a `_geo` field with `lat` and `lng`
- Filter by radius, bounding box, or polygon
- Sort by distance from a point

### 6.6 Multi-Search

Query multiple indexes in a single HTTP request:

```json
POST /multi-search
{
  "queries": [
    { "indexUid": "memories", "q": "deadline" },
    { "indexUid": "tools", "q": "deadline" }
  ]
}
```

### 6.7 Federated Search

Merge results from multiple indexes into a single ranked list (useful for searching across different document types).

### 6.8 Index Swapping

Atomically swap two indexes. Useful for zero-downtime reindexing:

```json
POST /swap-indexes
[{ "indexes": ["memories_v1", "memories_v2"] }]
```

### 6.9 Highlighting and Cropping

Search results can include formatted versions of matching fields with highlights and text excerpts cropped around matches.

### 6.10 Distinct Attribute

Deduplicate results based on a field value. For example, if multiple memories share a `source_id`, only the most relevant one is returned.

---

## 7. Deployment

### 7.1 Docker (Recommended for development and production)

```bash
# Basic
docker run -d --name meilisearch \
  -p 7700:7700 \
  -v $(pwd)/meili_data:/meili_data \
  -e MEILI_MASTER_KEY='strong-key-at-least-16-chars' \
  -e MEILI_ENV='production' \
  getmeili/meilisearch:latest

# With specific version tag (recommended)
docker run -d --name meilisearch \
  -p 7700:7700 \
  -v $(pwd)/meili_data:/meili_data \
  -e MEILI_MASTER_KEY='strong-key-at-least-16-chars' \
  -e MEILI_ENV='production' \
  getmeili/meilisearch:v1.12
```

**Critical:** Always mount a volume (`-v`) for `/meili_data`. Without it, data is lost when the container stops.

### 7.2 Key Environment Variables

| Variable                        | Purpose                                 | Default          |
| ------------------------------- | --------------------------------------- | ---------------- |
| `MEILI_MASTER_KEY`              | Master API key (required in production) | none             |
| `MEILI_ENV`                     | `development` or `production`           | `development`    |
| `MEILI_DB_PATH`                 | Database storage path                   | `./data.ms`      |
| `MEILI_HTTP_ADDR`               | Listen address                          | `localhost:7700` |
| `MEILI_HTTP_PAYLOAD_SIZE_LIMIT` | Max upload size                         | `100MB`          |
| `MEILI_LOG_LEVEL`               | Log verbosity                           | `INFO`           |
| `MEILI_NO_ANALYTICS`            | Disable telemetry                       | `false`          |

### 7.3 Kubernetes

Official Helm chart: `github.com/meilisearch/meilisearch-kubernetes`

```bash
helm repo add meilisearch https://meilisearch.github.io/meilisearch-kubernetes
helm install meilisearch meilisearch/meilisearch \
  --set environment.MEILI_ENV=production \
  --set environment.MEILI_MASTER_KEY=your-key \
  --set persistence.enabled=true \
  --set persistence.size=10Gi
```

For hive-server's existing k8s setup, Meilisearch would be a sidecar or separate Deployment with a PersistentVolumeClaim.

### 7.4 Meilisearch Cloud

Managed hosting from Meilisearch themselves. Pricing models:

- **Subscription-based:** Fixed monthly cost
- **Resource-based:** Pay for what you use

### 7.5 Backups

- **Dumps:** Portable exports of all indexes, documents, and settings. Importable into any Meilisearch version.
- **Snapshots:** Binary copies of the database for fast restoration. Version-specific.

```bash
# Create dump
curl -X POST http://localhost:7700/dumps -H "Authorization: Bearer $KEY"

# Schedule periodic snapshots
meilisearch --schedule-snapshot --snapshot-dir /meili_data/snapshots
```

---

## 8. Comparison to Alternatives

### 8.1 Meilisearch vs Elasticsearch

| Aspect             | Meilisearch                      | Elasticsearch                          |
| ------------------ | -------------------------------- | -------------------------------------- |
| **Language**       | Rust                             | Java                                   |
| **License**        | MIT (CE) / Commercial (EE)       | AGPLv3                                 |
| **Deployment**     | Single binary, no cluster needed | Requires JVM, cluster setup            |
| **Search speed**   | < 50ms (instant search)          | Varies, can be slow for instant search |
| **Typo tolerance** | Built-in, automatic              | Requires fuzzy query configuration     |
| **Learning curve** | Low (hours to set up)            | High (weeks/months)                    |
| **Scaling**        | Single-node (CE), Sharding (EE)  | Distributed by default                 |
| **Use cases**      | User-facing search               | Search + analytics + logging           |
| **Max index size** | ~80 TiB                          | Unlimited                              |
| **Memory model**   | Disk + mmap                      | RAM-heavy                              |
| **Aggregations**   | Limited (facets)                 | Full analytics engine                  |

**Summary:** Meilisearch is vastly simpler to operate and faster for user-facing search. Elasticsearch is more flexible for analytics, logging, and complex aggregation workloads.

### 8.2 Meilisearch vs Algolia

| Aspect             | Meilisearch                   | Algolia                        |
| ------------------ | ----------------------------- | ------------------------------ |
| **License**        | Open source (MIT)             | Proprietary SaaS               |
| **Hosting**        | Self-hosted or Cloud          | SaaS only                      |
| **Pricing**        | Free (CE), affordable (Cloud) | Usage-based, escalates quickly |
| **Max index size** | ~80 TiB                       | 128 GB                         |
| **Features**       | Very similar feature set      | Slightly more mature           |
| **Vendor lock-in** | None                          | High                           |
| **Community**      | Growing                       | Established                    |

**Summary:** Meilisearch is essentially an open-source Algolia. Nearly feature-equivalent with lower cost and no vendor lock-in.

### 8.3 Meilisearch vs Typesense

Both are modern, Algolia-alternative search engines. Typesense is written in C++, Meilisearch in Rust. Feature sets are very similar. Meilisearch has a larger community and more SDKs. Typesense has built-in clustering in its open-source version.

---

## 9. Limitations

### 9.1 Hard Limits

| Constraint                      | Limit                                   |
| ------------------------------- | --------------------------------------- |
| Documents per index             | 4,294,967,296 (2^32)                    |
| Attributes per document         | 65,536                                  |
| Positions per attribute         | 65,535 words                            |
| Primary key value size          | 511 bytes                               |
| Filterable attribute value size | 468 bytes                               |
| Query terms considered          | 10 (words after 10th are ignored)       |
| Concurrent search requests      | 1,000                                   |
| Default max results returned    | 1,000 (configurable via `maxTotalHits`) |
| Default payload upload size     | 100 MB (configurable)                   |
| Cloud upload size               | 20 MB                                   |
| Filter nesting depth            | 2,000                                   |
| Integer precision               | -2^53 to 2^53                           |
| Facets per search               | 100                                     |
| Database size (recommended)     | Under 2 TiB for performance             |

### 9.2 Architectural Limitations

- **Single-node only (Community Edition):** No built-in clustering or replication. The Enterprise Edition adds sharding but requires a commercial license.
- **No partial document retrieval by query:** Search always returns full documents (within `displayedAttributes`). No projection/aggregation like SQL.
- **No joins or relations:** Each index is independent. Cross-index queries use multi-search or federated search but cannot join on fields.
- **10-word query limit:** Queries longer than 10 words have the excess terms silently dropped. This is relevant for LLM-generated queries which tend to be verbose.
- **Asynchronous writes only:** You cannot write-and-read in the same request. There is always a delay (usually milliseconds to seconds) between adding a document and it being searchable.
- **Single-threaded indexing per index:** Tasks for a given index are processed sequentially. Cross-index parallelism exists, but a single index's indexing pipeline is serial.
- **No SQL or complex query language:** The filter syntax is limited compared to SQL. No GROUP BY, HAVING, subqueries, etc.
- **Synonyms don't apply to filters or facets.**
- **Facet search is single-term only** (does not support multi-word facet queries).

### 9.3 Operational Considerations

- Meilisearch re-indexes all documents when settings change (not incremental for setting changes).
- Large batch indexing (>3.5 GB) can cause internal errors; split into smaller batches.
- The task database grows over time (auto-cleanup at 1M+ entries / 10 GiB).
- Memory usage can spike during indexing due to the memory-mapped architecture.

---

## 10. Go Server Integration Patterns

### 10.1 Architecture for hive-server

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│   LLM Agents     │────>│   hive-server    │────>│   Meilisearch    │
│                  │<────│   (Go + chi)     │<────│   (port 7700)    │
└──────────────────┘     │                  │     └──────────────────┘
                         │   SQLite (primary │
                         │   data store)     │
                         └──────────────────┘
```

hive-server keeps SQLite as the primary data store and source of truth. Meilisearch acts as a **secondary search index** that is populated from SQLite data. This is the recommended pattern -- Meilisearch is not a database.

### 10.2 Integration in the Store Layer

Define a search interface alongside the existing store interface:

```go
// internal/search/search.go
package search

type SearchResult struct {
    ID             string
    Score          float64
    Highlights     map[string]string
}

type SearchRequest struct {
    Query   string
    Filters map[string]interface{}
    Sort    []string
    Limit   int
    Offset  int
    AgentID string  // for multi-tenancy filtering
}

type Searcher interface {
    Search(ctx context.Context, index string, req SearchRequest) ([]SearchResult, error)
    Index(ctx context.Context, index string, documents []interface{}) error
    Delete(ctx context.Context, index string, ids []string) error
    Configure(ctx context.Context, index string, settings interface{}) error
}
```

### 10.3 Meilisearch Implementation

```go
// internal/search/meilisearch.go
package search

import (
    "context"
    meilisearch "github.com/meilisearch/meilisearch-go"
)

type MeiliSearcher struct {
    client meilisearch.ServiceManager
}

func NewMeiliSearcher(host, apiKey string) *MeiliSearcher {
    client := meilisearch.New(host, meilisearch.WithAPIKey(apiKey))
    return &MeiliSearcher{client: client}
}

func (m *MeiliSearcher) Search(ctx context.Context, index string, req SearchRequest) ([]SearchResult, error) {
    searchReq := &meilisearch.SearchRequest{
        Limit:            int64(req.Limit),
        Offset:           int64(req.Offset),
        ShowRankingScore: true,
    }

    // Apply agent-scoped filter for multi-tenancy
    if req.AgentID != "" {
        searchReq.Filter = fmt.Sprintf("agent_id = '%s'", req.AgentID)
    }

    resp, err := m.client.Index(index).Search(req.Query, searchReq)
    if err != nil {
        return nil, fmt.Errorf("meilisearch search: %w", err)
    }

    results := make([]SearchResult, len(resp.Hits))
    for i, hit := range resp.Hits {
        h := hit.(map[string]interface{})
        results[i] = SearchResult{
            ID:    fmt.Sprint(h["id"]),
            Score: h["_rankingScore"].(float64),
        }
    }
    return results, nil
}
```

### 10.4 Sync Pattern (SQLite -> Meilisearch)

```go
// On document create/update in SQLite, also index in Meilisearch
func (s *Store) CreateMemory(ctx context.Context, mem Memory) error {
    // 1. Write to SQLite (source of truth)
    if err := s.db.Create(&mem); err != nil {
        return err
    }
    // 2. Index in Meilisearch (async, best-effort)
    go func() {
        s.searcher.Index(context.Background(), "memories", []interface{}{mem})
    }()
    return nil
}
```

### 10.5 Configuration via Environment

Following hive-server's existing pattern of env-var configuration:

```go
// MEILI_URL defaults to http://localhost:7700
// MEILI_API_KEY for authentication
// MEILI_ENABLED to toggle search (graceful degradation if not available)
```

---

## 11. Multi-Tenancy

### 11.1 Recommended Approach: Shared Index + Filter

Meilisearch recommends storing all tenants' data in a **single shared index** with a tenant identifier field (e.g., `agent_id`), rather than creating separate indexes per tenant. Reasons:

- Meilisearch processes tasks one index at a time; many indexes cause serialization bottlenecks.
- A single index with a filter field is more efficient.

### 11.2 Tenant Tokens

Tenant tokens are JWTs generated server-side that embed filter rules:

```go
// Generate tenant token for an agent
token, err := client.GenerateTenantToken(
    apiKeyUID,
    map[string]interface{}{
        "memories": map[string]interface{}{
            "filter": fmt.Sprintf("agent_id = '%s'", agentID),
        },
    },
    &meilisearch.TenantTokenOptions{
        ExpiresAt: time.Now().Add(1 * time.Hour),
        APIKey:    searchAPIKey,
    },
)
```

- Tokens grant search-only access.
- Tokens embed mandatory filters (e.g., `agent_id = 'agent-1'`) that cannot be bypassed.
- Tokens are short-lived and not stored by Meilisearch.
- Ideal for scenarios where agents search directly against Meilisearch.

### 11.3 For hive-server

Since hive-server acts as the API gateway between agents and storage, the simpler approach is to **inject agent-scoped filters server-side** in the handler layer, using the existing `X-Agent-ID` header middleware:

```go
func (h *Handler) SearchMemories(w http.ResponseWriter, r *http.Request) {
    agentID := r.Context().Value("agent_id").(string)
    query := r.URL.Query().Get("q")

    results, err := h.searcher.Search(r.Context(), "memories", search.SearchRequest{
        Query:   query,
        AgentID: agentID,  // Injected as filter: "agent_id = 'xxx'"
    })
    // ...
}
```

This is simpler than tenant tokens and consistent with hive-server's existing auth model.

---

## 12. Document Storage vs Pure Search

### 12.1 Meilisearch is NOT a Database

Meilisearch stores complete document copies in its index for retrieval, but it is explicitly designed as a **search engine, not a primary data store**:

- No ACID transactions
- No referential integrity
- No complex queries (joins, aggregations beyond facets)
- Asynchronous writes (eventual consistency)
- Re-indexes all documents on settings changes
- No backup guarantees comparable to a database

### 12.2 What It Stores

When you index a document, Meilisearch stores:

- The **full document** (all fields, up to `displayedAttributes`)
- The **inverted index** for searchable fields
- **Filter indexes** for filterable fields
- **Sort indexes** for sortable fields
- **Vector embeddings** if AI search is configured

### 12.3 Recommended Pattern for hive-server

| Concern                                             | Component                                                  |
| --------------------------------------------------- | ---------------------------------------------------------- |
| Source of truth, CRUD, relations                    | SQLite                                                     |
| Full-text search, typo tolerance, relevancy ranking | Meilisearch                                                |
| Memory retrieval by ID                              | SQLite                                                     |
| Memory search by content/tags                       | Meilisearch (returns IDs) -> SQLite (fetches full records) |

**Option A (simpler):** Meilisearch returns full document copies. No round-trip to SQLite for search results. Requires keeping Meilisearch in sync.

**Option B (more reliable):** Meilisearch returns only document IDs and scores. hive-server uses those IDs to fetch from SQLite. Guarantees freshness but adds latency.

Option A is the common pattern and is recommended unless there are strict consistency requirements.

---

## 13. Relevance to hive-server

### 13.1 Use Cases for Meilisearch in hive-server

1. **Memory search:** Agents search their accumulated memories by content, tags, and metadata with typo tolerance and relevancy ranking.
2. **Tool discovery:** Agents search for available tools by name, description, and capability with natural-language-like queries.
3. **Cross-agent knowledge retrieval:** With proper scoping, agents could search shared knowledge bases.
4. **Semantic/hybrid search:** Using Meilisearch's embedder support, agents could find semantically related memories even when exact keywords don't match -- critical for LLM memory systems.

### 13.2 Integration Effort Estimate

| Task                                                 | Effort        |
| ---------------------------------------------------- | ------------- |
| Add `meilisearch-go` dependency                      | Minimal       |
| Define search interface + Meilisearch implementation | 1-2 days      |
| Add index configuration/migration on startup         | Half day      |
| Add sync hooks to existing store write paths         | 1 day         |
| Add search endpoints to handlers                     | 1 day         |
| Add multi-tenancy filters using existing X-Agent-ID  | Half day      |
| Tests with SDK mocks                                 | 1 day         |
| Docker Compose / k8s deployment for Meilisearch      | Half day      |
| **Total**                                            | **~5-7 days** |

### 13.3 Recommended Index Schema (Example)

```go
// memories index
settings := &meilisearch.Settings{
    SearchableAttributes: []string{"content", "title", "tags"},
    FilterableAttributes: []string{"agent_id", "type", "priority", "created_at", "source"},
    SortableAttributes:   []string{"created_at", "priority", "updated_at"},
    DisplayedAttributes:  []string{"id", "content", "title", "agent_id", "type",
                                    "priority", "tags", "created_at", "updated_at"},
    TypoTolerance: &meilisearch.TypoTolerance{
        Enabled: true,
    },
    Synonyms: map[string][]string{
        "task":   {"todo", "action", "work item"},
        "bug":    {"issue", "defect", "problem"},
        "memory": {"note", "recollection", "context"},
    },
}
```

### 13.4 Risks and Mitigations

| Risk                                            | Mitigation                                                                                                                  |
| ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| 10-word query limit problematic for LLM queries | Preprocess queries: extract key terms before sending to Meilisearch; use hybrid/semantic search for meaning-based retrieval |
| Meilisearch downtime                            | Graceful degradation: fall back to SQLite LIKE queries when Meilisearch is unavailable                                      |
| Data sync drift between SQLite and Meilisearch  | Periodic full re-sync job; idempotent document upserts                                                                      |
| Memory usage spikes during indexing             | Resource limits in k8s; separate Meilisearch pod from hive-server                                                           |
| mTLS incompatibility with Go                    | Use plain TLS or network-level security (k8s network policies) instead of mTLS                                              |

---

## Sources

- [What is Meilisearch? - Official Documentation](https://www.meilisearch.com/docs/learn/getting_started/what_is_meilisearch)
- [Meilisearch Homepage](https://www.meilisearch.com/)
- [Known Limitations](https://www.meilisearch.com/docs/learn/resources/known_limitations)
- [Built-in Ranking Rules](https://www.meilisearch.com/docs/learn/relevancy/ranking_rules)
- [Typo Tolerance Calculations](https://www.meilisearch.com/docs/learn/relevancy/typo_tolerance_calculations)
- [Search API Reference](https://www.meilisearch.com/docs/reference/api/search)
- [Settings API Reference](https://www.meilisearch.com/docs/reference/api/settings)
- [Keys API Reference](https://www.meilisearch.com/docs/reference/api/keys)
- [Indexes Documentation](https://www.meilisearch.com/docs/learn/getting_started/indexes)
- [Comparison to Alternatives](https://www.meilisearch.com/docs/learn/resources/comparison_to_alternatives)
- [Multitenancy and Tenant Tokens](https://www.meilisearch.com/docs/learn/security/multitenancy_tenant_tokens)
- [Multi-tenancy Guide (Blog)](https://www.meilisearch.com/blog/multi-tenancy)
- [Docker Guide](https://www.meilisearch.com/docs/guides/docker)
- [Kubernetes Helm Charts](https://github.com/meilisearch/meilisearch-kubernetes)
- [meilisearch-go SDK (GitHub)](https://github.com/meilisearch/meilisearch-go)
- [meilisearch-go SDK (pkg.go.dev)](https://pkg.go.dev/github.com/meilisearch/meilisearch-go)
- [meilisearch-go Basic Usage (DeepWiki)](https://deepwiki.com/meilisearch/meilisearch-go/2.3-basic-usage-examples)
- [meilisearch-go Settings Management (DeepWiki)](https://deepwiki.com/meilisearch/meilisearch-go/5.4-settings-management)
- [Hybrid Search](https://www.meilisearch.com/solutions/hybrid-search)
- [AI-Powered Search](https://www.meilisearch.com/docs/learn/ai_powered_search/getting_started_with_ai_search)
- [Horizontal Scaling with Sharding](https://www.meilisearch.com/blog/horizontal-scaling-with-sharding)
- [Enterprise License Announcement](https://www.meilisearch.com/blog/enterprise-license)
- [From Ranking to Scoring](https://www.meilisearch.com/blog/from-ranking-to-scoring)
- [Filtering and Faceted Search](https://www.meilisearch.com/docs/learn/filtering_and_sorting/search_with_facet_filters)
- [Sorting Documentation](https://www.meilisearch.com/docs/learn/fine_tuning_results/sorting)
- [Top 10 Elasticsearch Alternatives in 2026](https://www.meilisearch.com/blog/elasticsearch-alternatives)
- [Typesense vs Algolia vs Elasticsearch vs Meilisearch Comparison](https://typesense.org/typesense-vs-algolia-vs-elasticsearch-vs-meilisearch/)
- [Building a Search Engine in Golang](https://www.meilisearch.com/blog/golang-search-engine)
- [Meilisearch September 2025 Updates](https://www.meilisearch.com/blog/september-2025-updates)
- [Meilisearch January 2026 Updates](https://www.meilisearch.com/blog/January-2026-updates)
