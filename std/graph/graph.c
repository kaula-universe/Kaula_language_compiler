#include "graph.h"
#include "../string/string.h"
#include <stdlib.h>
#include <string.h>

Graph* graph_create(bool_t directed) {
    Graph* g = (Graph*)kmm_v4_malloc(sizeof(Graph));
    if (!g) return NULL;
    g->nodes = NULL;
    g->node_count = 0;
    g->node_capacity = 0;
    g->directed = directed;
    return g;
}

void graph_destroy(Graph* graph) {
    if (!graph) return;
    for (size_t i = 0; i < graph->node_count; i++) {
        GraphNode* n = graph->nodes[i];
        kmm_v4_free(n->name.data);
        kmm_v4_free(n->edges);
        kmm_v4_free(n);
    }
    kmm_v4_free(graph->nodes);
    kmm_v4_free(graph);
}

GraphNode* graph_add_node(Graph* graph, const char* name, void* data) {
    if (!graph || !name) return NULL;
    if (graph_find_node(graph, name)) return NULL;
    
    if (graph->node_count >= graph->node_capacity) {
        graph->node_capacity = graph->node_capacity == 0 ? 16 : graph->node_capacity * 2;
        graph->nodes = (GraphNode**)kmm_v4_realloc(graph->nodes, graph->node_capacity * sizeof(GraphNode*));
        if (!graph->nodes) return NULL;
    }
    
    GraphNode* node = (GraphNode*)kmm_v4_malloc(sizeof(GraphNode));
    if (!node) return NULL;
    node->name.data = string_copy(name);
    node->name.len = strlen(name);
    node->data = data;
    node->edges = NULL;
    node->edge_count = 0;
    node->edge_capacity = 0;
    
    graph->nodes[graph->node_count++] = node;
    return node;
}

GraphNode* graph_find_node(const Graph* graph, const char* name) {
    if (!graph || !name) return NULL;
    for (size_t i = 0; i < graph->node_count; i++) {
        if (strcmp(graph->nodes[i]->name.data, name) == 0) {
            return graph->nodes[i];
        }
    }
    return NULL;
}

void graph_add_edge(Graph* graph, const char* from, const char* to, i64 weight) {
    GraphNode* src = graph_find_node(graph, from);
    GraphNode* dst = graph_find_node(graph, to);
    if (!src || !dst) return;
    
    if (src->edge_count >= src->edge_capacity) {
        src->edge_capacity = src->edge_capacity == 0 ? 8 : src->edge_capacity * 2;
        src->edges = (GraphEdge*)kmm_v4_realloc(src->edges, src->edge_capacity * sizeof(GraphEdge));
        if (!src->edges) return;
    }
    
    for (size_t i = 0; i < src->edge_count; i++) {
        if (src->edges[i].target == dst) {
            src->edges[i].weight = weight;
            return;
        }
    }
    
    src->edges[src->edge_count++] = (GraphEdge){dst, weight};
    
    if (!graph->directed) {
        if (dst->edge_count >= dst->edge_capacity) {
            dst->edge_capacity = dst->edge_capacity == 0 ? 8 : dst->edge_capacity * 2;
            dst->edges = (GraphEdge*)kmm_v4_realloc(dst->edges, dst->edge_capacity * sizeof(GraphEdge));
            if (!dst->edges) return;
        }
        dst->edges[dst->edge_count++] = (GraphEdge){src, weight};
    }
}

void graph_remove_edge(Graph* graph, const char* from, const char* to) {
    GraphNode* src = graph_find_node(graph, from);
    GraphNode* dst = graph_find_node(graph, to);
    if (!src || !dst) return;
    
    for (size_t i = 0; i < src->edge_count; i++) {
        if (src->edges[i].target == dst) {
            for (size_t j = i; j < src->edge_count - 1; j++) {
                src->edges[j] = src->edges[j + 1];
            }
            src->edge_count--;
            break;
        }
    }
    
    if (!graph->directed) {
        for (size_t i = 0; i < dst->edge_count; i++) {
            if (dst->edges[i].target == src) {
                for (size_t j = i; j < dst->edge_count - 1; j++) {
                    dst->edges[j] = dst->edges[j + 1];
                }
                dst->edge_count--;
                break;
            }
        }
    }
}

bool_t graph_has_edge(const Graph* graph, const char* from, const char* to) {
    GraphNode* src = graph_find_node(graph, from);
    GraphNode* dst = graph_find_node(graph, to);
    if (!src || !dst) return false;
    
    for (size_t i = 0; i < src->edge_count; i++) {
        if (src->edges[i].target == dst) return true;
    }
    return false;
}

i64 graph_get_edge_weight(const Graph* graph, const char* from, const char* to) {
    GraphNode* src = graph_find_node(graph, from);
    GraphNode* dst = graph_find_node(graph, to);
    if (!src || !dst) return INT64_MIN;
    
    for (size_t i = 0; i < src->edge_count; i++) {
        if (src->edges[i].target == dst) return src->edges[i].weight;
    }
    return INT64_MIN;
}

size_t graph_out_degree(const Graph* graph, const char* node_name) {
    GraphNode* node = graph_find_node(graph, node_name);
    return node ? node->edge_count : 0;
}

size_t graph_in_degree(const Graph* graph, const char* node_name) {
    if (!graph) return 0;
    GraphNode* target = graph_find_node(graph, node_name);
    if (!target) return 0;
    
    size_t count = 0;
    for (size_t i = 0; i < graph->node_count; i++) {
        GraphNode* n = graph->nodes[i];
        for (size_t j = 0; j < n->edge_count; j++) {
            if (n->edges[j].target == target) count++;
        }
    }
    return count;
}

void graph_bfs(const Graph* graph, const char* start,
               void (*visitor)(const GraphNode*, i64, void*), void* ctx) {
    GraphNode* start_node = graph_find_node(graph, start);
    if (!graph || !start_node || !visitor) return;
    
    bool_t* visited = (bool_t*)kmm_v4_malloc(graph->node_count * sizeof(bool_t));
    if (!visited) return;
    memset(visited, 0, graph->node_count * sizeof(bool_t));
    
    GraphNode** queue = (GraphNode**)kmm_v4_malloc(graph->node_count * sizeof(GraphNode*));
    if (!queue) { kmm_v4_free(visited); return; }
    
    size_t front = 0, back = 0;
    queue[back++] = start_node;
    visited[0] = true;
    
    for (size_t i = 0; i < graph->node_count; i++) {
        if (graph->nodes[i] == start_node) { visited[i] = true; break; }
    }
    
    i64 level = 0;
    while (front < back) {
        size_t level_size = back - front;
        for (size_t i = 0; i < level_size; i++) {
            GraphNode* curr = queue[front++];
            visitor(curr, level, ctx);
            
            for (size_t j = 0; j < curr->edge_count; j++) {
                GraphNode* neighbor = curr->edges[j].target;
                bool_t found = false;
                size_t idx = 0;
                for (size_t k = 0; k < graph->node_count; k++) {
                    if (graph->nodes[k] == neighbor) { idx = k; found = true; break; }
                }
                if (found && !visited[idx]) {
                    visited[idx] = true;
                    queue[back++] = neighbor;
                }
            }
        }
        level++;
    }
    
    kmm_v4_free(queue);
    kmm_v4_free(visited);
}

void graph_dfs(const Graph* graph, const char* start,
               void (*visitor)(const GraphNode*, i64, void*), void* ctx) {
    GraphNode* start_node = graph_find_node(graph, start);
    if (!graph || !start_node || !visitor) return;
    
    bool_t* visited = (bool_t*)kmm_v4_malloc(graph->node_count * sizeof(bool_t));
    if (!visited) return;
    memset(visited, 0, graph->node_count * sizeof(bool_t));
    
    GraphNode** stack = (GraphNode**)kmm_v4_malloc(graph->node_count * sizeof(GraphNode*));
    if (!stack) { kmm_v4_free(visited); return; }
    
    size_t top = 0;
    stack[top++] = start_node;
    
    for (size_t i = 0; i < graph->node_count; i++) {
        if (graph->nodes[i] == start_node) { visited[i] = true; break; }
    }
    
    while (top > 0) {
        GraphNode* curr = stack[--top];
        visitor(curr, (i64)top, ctx);
        
        for (int j = (int)curr->edge_count - 1; j >= 0; j--) {
            GraphNode* neighbor = curr->edges[j].target;
            bool_t found = false;
            size_t idx = 0;
            for (size_t k = 0; k < graph->node_count; k++) {
                if (graph->nodes[k] == neighbor) { idx = k; found = true; break; }
            }
            if (found && !visited[idx]) {
                visited[idx] = true;
                stack[top++] = neighbor;
            }
        }
    }
    
    kmm_v4_free(stack);
    kmm_v4_free(visited);
}

i64* graph_dijkstra(const Graph* graph, const char* start, const char* end) {
    GraphNode* start_node = graph_find_node(graph, start);
    GraphNode* end_node = graph_find_node(graph, end);
    if (!graph || !start_node) return NULL;
    
    i64* dist = (i64*)kmm_v4_malloc(graph->node_count * sizeof(i64));
    bool_t* visited = (bool_t*)kmm_v4_malloc(graph->node_count * sizeof(bool_t));
    if (!dist || !visited) { kmm_v4_free(dist); kmm_v4_free(visited); return NULL; }
    
    for (size_t i = 0; i < graph->node_count; i++) {
        dist[i] = INT64_MAX;
        visited[i] = false;
    }
    
    for (size_t i = 0; i < graph->node_count; i++) {
        if (graph->nodes[i] == start_node) { dist[i] = 0; break; }
    }
    
    for (size_t count = 0; count < graph->node_count; count++) {
        size_t min_idx = (size_t)-1;
        i64 min_dist = INT64_MAX;
        for (size_t i = 0; i < graph->node_count; i++) {
            if (!visited[i] && dist[i] < min_dist) {
                min_dist = dist[i];
                min_idx = i;
            }
        }
        
        if (min_idx == (size_t)-1) break;
        visited[min_idx] = true;
        
        GraphNode* u = graph->nodes[min_idx];
        for (size_t j = 0; j < u->edge_count; j++) {
            GraphNode* v = u->edges[j].target;
            i64 weight = u->edges[j].weight;
            size_t v_idx = 0;
            for (size_t k = 0; k < graph->node_count; k++) {
                if (graph->nodes[k] == v) { v_idx = k; break; }
            }
            
            if (!visited[v_idx] && dist[min_idx] != INT64_MAX &&
                dist[min_idx] + weight < dist[v_idx]) {
                dist[v_idx] = dist[min_idx] + weight;
            }
        }
    }
    
    kmm_v4_free(visited);
    
    if (end_node) {
        i64* result = (i64*)kmm_v4_malloc(sizeof(i64));
        for (size_t i = 0; i < graph->node_count; i++) {
            if (graph->nodes[i] == end_node) { *result = dist[i]; break; }
        }
        kmm_v4_free(dist);
        return result;
    }
    return dist;
}

bool_t graph_bellman_ford(const Graph* graph, const char* start, i64* distances) {
    GraphNode* start_node = graph_find_node(graph, start);
    if (!graph || !start_node || !distances) return false;
    
    for (size_t i = 0; i < graph->node_count; i++) {
        distances[i] = INT64_MAX;
    }
    
    for (size_t i = 0; i < graph->node_count; i++) {
        if (graph->nodes[i] == start_node) { distances[i] = 0; break; }
    }
    
    for (size_t i = 1; i < graph->node_count; i++) {
        bool_t updated = false;
        for (size_t j = 0; j < graph->node_count; j++) {
            GraphNode* u = graph->nodes[j];
            for (size_t k = 0; k < u->edge_count; k++) {
                GraphNode* v = u->edges[k].target;
                i64 weight = u->edges[k].weight;
                
                size_t v_idx = 0;
                for (size_t l = 0; l < graph->node_count; l++) {
                    if (graph->nodes[l] == v) { v_idx = l; break; }
                }
                
                if (distances[j] != INT64_MAX && distances[j] + weight < distances[v_idx]) {
                    distances[v_idx] = distances[j] + weight;
                    updated = true;
                }
            }
        }
        if (!updated) break;
    }
    
    for (size_t j = 0; j < graph->node_count; j++) {
        GraphNode* u = graph->nodes[j];
        for (size_t k = 0; k < u->edge_count; k++) {
            GraphNode* v = u->edges[k].target;
            i64 weight = u->edges[k].weight;
            
            size_t v_idx = 0;
            for (size_t l = 0; l < graph->node_count; l++) {
                if (graph->nodes[l] == v) { v_idx = l; break; }
            }
            
            if (distances[j] != INT64_MAX && distances[j] + weight < distances[v_idx]) {
                return true;
            }
        }
    }
    return false;
}

bool_t graph_has_cycle(const Graph* graph) {
    if (!graph) return false;
    
    bool_t* visited = (bool_t*)kmm_v4_malloc(graph->node_count * sizeof(bool_t));
    bool_t* rec_stack = (bool_t*)kmm_v4_malloc(graph->node_count * sizeof(bool_t));
    if (!visited || !rec_stack) { kmm_v4_free(visited); kmm_v4_free(rec_stack); return false; }
    
    memset(visited, 0, graph->node_count * sizeof(bool_t));
    memset(rec_stack, 0, graph->node_count * sizeof(bool_t));
    
    bool_t has_cycle = false;
    
    for (size_t i = 0; i < graph->node_count && !has_cycle; i++) {
        if (!visited[i]) {
            GraphNode** stack = (GraphNode**)kmm_v4_malloc(graph->node_count * sizeof(GraphNode*));
            bool_t* in_stack = (bool_t*)kmm_v4_malloc(graph->node_count * sizeof(bool_t));
            if (!stack || !in_stack) { kmm_v4_free(stack); kmm_v4_free(in_stack); continue; }
            
            size_t top = 0;
            stack[top++] = graph->nodes[i];
            visited[i] = true;
            in_stack[i] = true;
            
            while (top > 0 && !has_cycle) {
                GraphNode* curr = stack[--top];
                size_t curr_idx = 0;
                for (size_t j = 0; j < graph->node_count; j++) {
                    if (graph->nodes[j] == curr) { curr_idx = j; break; }
                }
                
                for (size_t j = 0; j < curr->edge_count; j++) {
                    GraphNode* neighbor = curr->edges[j].target;
                    size_t neighbor_idx = 0;
                    for (size_t k = 0; k < graph->node_count; k++) {
                        if (graph->nodes[k] == neighbor) { neighbor_idx = k; break; }
                    }
                    
                    if (!visited[neighbor_idx]) {
                        visited[neighbor_idx] = true;
                        in_stack[neighbor_idx] = true;
                        stack[top++] = neighbor;
                    } else if (in_stack[neighbor_idx]) {
                        has_cycle = true;
                        break;
                    }
                }
                in_stack[curr_idx] = false;
            }
            
            kmm_v4_free(stack);
            kmm_v4_free(in_stack);
        }
    }
    
    kmm_v4_free(visited);
    kmm_v4_free(rec_stack);
    return has_cycle;
}

void graph_topological_sort(const Graph* graph, GraphNode*** result, size_t* count) {
    if (!graph || !result || !count) return;
    
    i64* in_deg = (i64*)kmm_v4_malloc(graph->node_count * sizeof(i64));
    if (!in_deg) return;
    
    for (size_t i = 0; i < graph->node_count; i++) {
        in_deg[i] = (i64)graph_in_degree(graph, graph->nodes[i]->name.data);
    }
    
    GraphNode** queue = (GraphNode**)kmm_v4_malloc(graph->node_count * sizeof(GraphNode*));
    GraphNode** res = (GraphNode**)kmm_v4_malloc(graph->node_count * sizeof(GraphNode*));
    if (!queue || !res) { kmm_v4_free(queue); kmm_v4_free(res); kmm_v4_free(in_deg); return; }
    
    size_t front = 0, back = 0, idx = 0;
    for (size_t i = 0; i < graph->node_count; i++) {
        if (in_deg[i] == 0) queue[back++] = graph->nodes[i];
    }
    
    while (front < back) {
        GraphNode* curr = queue[front++];
        res[idx++] = curr;
        
        size_t curr_idx = 0;
        for (size_t i = 0; i < graph->node_count; i++) {
            if (graph->nodes[i] == curr) { curr_idx = i; break; }
        }
        
        for (size_t j = 0; j < curr->edge_count; j++) {
            GraphNode* neighbor = curr->edges[j].target;
            size_t neighbor_idx = 0;
            for (size_t k = 0; k < graph->node_count; k++) {
                if (graph->nodes[k] == neighbor) { neighbor_idx = k; break; }
            }
            if (--in_deg[neighbor_idx] == 0) queue[back++] = neighbor;
        }
    }
    
    *result = res;
    *count = idx;
    
    kmm_v4_free(queue);
    kmm_v4_free(in_deg);
}

size_t graph_strongly_connected_components(const Graph* graph, GraphNode*** components, size_t* counts) {
    if (!graph || !components || !counts) return 0;
    
    bool_t* visited = (bool_t*)kmm_v4_malloc(graph->node_count * sizeof(bool_t));
    i64* order = (i64*)kmm_v4_malloc(graph->node_count * sizeof(i64));
    if (!visited || !order) { kmm_v4_free(visited); kmm_v4_free(order); return 0; }
    
    memset(visited, 0, graph->node_count * sizeof(bool_t));
    i64 idx = 0;
    
    for (size_t i = 0; i < graph->node_count; i++) {
        if (!visited[i]) {
            GraphNode** stack = (GraphNode**)kmm_v4_malloc(graph->node_count * sizeof(GraphNode*));
            size_t top = 0;
            stack[top++] = graph->nodes[i];
            visited[i] = true;
            
            while (top > 0) {
                GraphNode* curr = stack[--top];
                order[idx++] = (i64)curr;
                
                for (size_t j = 0; j < curr->edge_count; j++) {
                    GraphNode* neighbor = curr->edges[j].target;
                    size_t n_idx = 0;
                    for (size_t k = 0; k < graph->node_count; k++) {
                        if (graph->nodes[k] == neighbor) { n_idx = k; break; }
                    }
                    if (!visited[n_idx]) {
                        visited[n_idx] = true;
                        stack[top++] = neighbor;
                    }
                }
            }
            kmm_v4_free(stack);
        }
    }
    
    Graph* transpose = graph_create(true);
    for (size_t i = 0; i < graph->node_count; i++) {
        graph_add_node(transpose, graph->nodes[i]->name.data, NULL);
    }
    for (size_t i = 0; i < graph->node_count; i++) {
        GraphNode* u = graph->nodes[i];
        for (size_t j = 0; j < u->edge_count; j++) {
            graph_add_edge(transpose, u->edges[j].target->name.data, u->name.data, u->edges[j].weight);
        }
    }
    
    memset(visited, 0, graph->node_count * sizeof(bool_t));
    size_t comp_count = 0;
    
    for (i64 i = (i64)graph->node_count - 1; i >= 0; i--) {
        GraphNode* start = (GraphNode*)order[i];
        size_t start_idx = 0;
        for (size_t j = 0; j < graph->node_count; j++) {
            if (graph->nodes[j] == start) { start_idx = j; break; }
        }
        
        if (!visited[start_idx]) {
            GraphNode** comp = (GraphNode**)kmm_v4_malloc(graph->node_count * sizeof(GraphNode*));
            size_t c_idx = 0;
            
            GraphNode** stack = (GraphNode**)kmm_v4_malloc(graph->node_count * sizeof(GraphNode*));
            size_t top = 0;
            stack[top++] = start;
            visited[start_idx] = true;
            
            while (top > 0) {
                GraphNode* curr = stack[--top];
                comp[c_idx++] = curr;
                
                for (size_t j = 0; j < graph->node_count; j++) {
                    if (graph->nodes[j] == curr) {
                        GraphNode* trans_node = graph_find_node(transpose, curr->name.data);
                        for (size_t k = 0; k < trans_node->edge_count; k++) {
                            GraphNode* neighbor = trans_node->edges[k].target;
                            size_t n_idx = 0;
                            for (size_t l = 0; l < graph->node_count; l++) {
                                if (graph->nodes[l] == neighbor) { n_idx = l; break; }
                            }
                            if (!visited[n_idx]) {
                                visited[n_idx] = true;
                                stack[top++] = neighbor;
                            }
                        }
                        break;
                    }
                }
            }
            
            kmm_v4_free(stack);
            components[comp_count] = comp;
            counts[comp_count] = c_idx;
            comp_count++;
        }
    }
    
    graph_destroy(transpose);
    kmm_v4_free(order);
    kmm_v4_free(visited);
    return comp_count;
}