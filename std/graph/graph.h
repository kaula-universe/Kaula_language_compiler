#pragma once
#include "../base/types.h"
#include "../memory/memory.h"

typedef struct GraphNode {
    String name;
    void* data;
    struct GraphEdge* edges;
    size_t edge_count;
    size_t edge_capacity;
} GraphNode;

typedef struct GraphEdge {
    GraphNode* target;
    i64 weight;
} GraphEdge;

typedef struct Graph {
    GraphNode** nodes;
    size_t node_count;
    size_t node_capacity;
    bool_t directed;
} Graph;

Graph* graph_create(bool_t directed);
void graph_destroy(Graph* graph);

GraphNode* graph_add_node(Graph* graph, const char* name, void* data);
GraphNode* graph_find_node(const Graph* graph, const char* name);

void graph_add_edge(Graph* graph, const char* from, const char* to, i64 weight);
void graph_remove_edge(Graph* graph, const char* from, const char* to);

bool_t graph_has_edge(const Graph* graph, const char* from, const char* to);
i64 graph_get_edge_weight(const Graph* graph, const char* from, const char* to);

size_t graph_out_degree(const Graph* graph, const char* node_name);
size_t graph_in_degree(const Graph* graph, const char* node_name);

void graph_bfs(const Graph* graph, const char* start,
               void (*visitor)(const GraphNode*, i64, void*), void* ctx);
void graph_dfs(const Graph* graph, const char* start,
               void (*visitor)(const GraphNode*, i64, void*), void* ctx);

i64* graph_dijkstra(const Graph* graph, const char* start, const char* end);
bool_t graph_bellman_ford(const Graph* graph, const char* start, i64* distances);

bool_t graph_has_cycle(const Graph* graph);
void graph_topological_sort(const Graph* graph, GraphNode*** result, size_t* count);

size_t graph_strongly_connected_components(const Graph* graph, GraphNode*** components, size_t* counts);