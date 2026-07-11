#include "regex.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <string.h>
#include <stdlib.h>
#include <stdio.h>

#define MAX_STATES 256
#define MAX_GROUPS 32

typedef enum {
    NFA_CHAR,
    NFA_ANY,
    NFA_SPLIT,
    NFA_MATCH,
    NFA_CHARSET,
    NFA_NEG_CHARSET
} NfaStateType;

typedef struct NfaState {
    NfaStateType type;
    char c;
    unsigned char charset[32];
    struct NfaState* out;
    struct NfaState* out2;
    int group_id;
    int group_end;
} NfaState;

typedef struct {
    NfaState* start;
    NfaState** states;
    int state_count;
    int group_count;
    char error_msg[256];
} Nfa;

typedef struct {
    NfaState** states;
    int count;
    int capacity;
} StateSet;

typedef struct {
    size_t start;
    size_t end;
} GroupMatch;

static void charset_set_bit(unsigned char* charset, char c) {
    unsigned char uc = (unsigned char)c;
    charset[uc / 8] |= (1 << (uc % 8));
}

static int charset_test_bit(const unsigned char* charset, char c) {
    unsigned char uc = (unsigned char)c;
    return (charset[uc / 8] & (1 << (uc % 8))) != 0;
}

static void charset_clear(unsigned char* charset) {
    memset(charset, 0, 32);
}

static void charset_add_range(unsigned char* charset, char start, char end) {
    int i;
    for (i = (unsigned char)start; i <= (unsigned char)end; i++) {
        charset_set_bit(charset, (char)i);
    }
}

static NfaState* nfa_state_new(Nfa* nfa, NfaStateType type) {
    NfaState* s = (NfaState*)kmm_v4_malloc(sizeof(NfaState));
    if (!s) return NULL;
    memset(s, 0, sizeof(NfaState));
    s->type = type;
    s->out = NULL;
    s->out2 = NULL;
    s->group_id = -1;
    s->group_end = -1;
    if (nfa->state_count < MAX_STATES) {
        nfa->states[nfa->state_count++] = s;
    }
    return s;
}

static StateSet* state_set_new(int capacity) {
    StateSet* set = (StateSet*)kmm_v4_malloc(sizeof(StateSet));
    if (!set) return NULL;
    set->states = (NfaState**)kmm_v4_malloc(sizeof(NfaState*) * capacity);
    if (!set->states) {
        kmm_v4_free(set);
        return NULL;
    }
    set->count = 0;
    set->capacity = capacity;
    return set;
}

static void state_set_free(StateSet* set) {
    if (set) {
        if (set->states) kmm_v4_free(set->states);
        kmm_v4_free(set);
    }
}

static void state_set_add(StateSet* set, NfaState* state) {
    int i;
    if (!state) return;
    for (i = 0; i < set->count; i++) {
        if (set->states[i] == state) return;
    }
    if (set->count < set->capacity) {
        set->states[set->count++] = state;
    }
}

static void epsilon_closure(NfaState* state, StateSet* set, int* visited) {
    int id;
    if (!state) return;
    id = (int)(size_t)state;
    for (int i = 0; i < set->count; i++) {
        if (set->states[i] == state) return;
    }
    state_set_add(set, state);
    if (state->type == NFA_SPLIT) {
        epsilon_closure(state->out, set, visited);
        epsilon_closure(state->out2, set, visited);
    }
}

static StateSet* build_start_set(Nfa* nfa) {
    StateSet* set = state_set_new(MAX_STATES);
    if (!set) return NULL;
    int visited[MAX_STATES] = {0};
    epsilon_closure(nfa->start, set, visited);
    return set;
}

static StateSet* step(StateSet* current, char c, GroupMatch* groups, size_t pos) {
    StateSet* next = state_set_new(MAX_STATES);
    if (!next) return NULL;
    int visited[MAX_STATES] = {0};
    int i;
    for (i = 0; i < current->count; i++) {
        NfaState* s = current->states[i];
        if (!s) continue;
        if (s->type == NFA_CHAR && s->c == c) {
            epsilon_closure(s->out, next, visited);
        } else if (s->type == NFA_ANY) {
            epsilon_closure(s->out, next, visited);
        } else if (s->type == NFA_CHARSET) {
            if (charset_test_bit(s->charset, c)) {
                epsilon_closure(s->out, next, visited);
            }
        } else if (s->type == NFA_NEG_CHARSET) {
            if (!charset_test_bit(s->charset, c)) {
                epsilon_closure(s->out, next, visited);
            }
        }
    }
    return next;
}

static int has_match(StateSet* set) {
    int i;
    for (i = 0; i < set->count; i++) {
        if (set->states[i] && set->states[i]->type == NFA_MATCH) {
            return 1;
        }
    }
    return 0;
}

typedef struct RegexParser {
    const char* pattern;
    int pos;
    Nfa* nfa;
    int group_counter;
} RegexParser;

static NfaState* parse_expr(RegexParser* parser);

static char peek(RegexParser* parser) {
    return parser->pattern[parser->pos];
}

static char advance(RegexParser* parser) {
    return parser->pattern[parser->pos++];
}

static int match_char(RegexParser* parser, char c) {
    if (peek(parser) == c) {
        advance(parser);
        return 1;
    }
    return 0;
}

static int parse_char_class(RegexParser* parser, unsigned char* charset) {
    int negated = 0;
    char c;
    if (peek(parser) == '^') {
        negated = 1;
        advance(parser);
    }
    while (peek(parser) && peek(parser) != ']') {
        char start = advance(parser);
        if (start == '\\' && peek(parser)) {
            char esc = advance(parser);
            switch (esc) {
                case 'd':
                    charset_add_range(charset, '0', '9');
                    break;
                case 'w':
                    charset_add_range(charset, 'a', 'z');
                    charset_add_range(charset, 'A', 'Z');
                    charset_add_range(charset, '0', '9');
                    charset_set_bit(charset, '_');
                    break;
                case 's':
                    charset_set_bit(charset, ' ');
                    charset_set_bit(charset, '\t');
                    charset_set_bit(charset, '\n');
                    charset_set_bit(charset, '\r');
                    charset_set_bit(charset, '\f');
                    charset_set_bit(charset, '\v');
                    break;
                case 'n':
                    charset_set_bit(charset, '\n');
                    break;
                case 't':
                    charset_set_bit(charset, '\t');
                    break;
                case 'r':
                    charset_set_bit(charset, '\r');
                    break;
                case '\\':
                    charset_set_bit(charset, '\\');
                    break;
                case ']':
                    charset_set_bit(charset, ']');
                    break;
                case '^':
                    charset_set_bit(charset, '^');
                    break;
                case '-':
                    charset_set_bit(charset, '-');
                    break;
                default:
                    charset_set_bit(charset, esc);
                    break;
            }
        } else if (peek(parser) == '-' && parser->pattern[parser->pos + 1] && parser->pattern[parser->pos + 1] != ']') {
            advance(parser);
            char end = advance(parser);
            if ((unsigned char)start <= (unsigned char)end) {
                charset_add_range(charset, start, end);
            }
        } else {
            charset_set_bit(charset, start);
        }
    }
    if (!match_char(parser, ']')) {
        return -1;
    }
    return negated;
}

static NfaState* parse_atom(RegexParser* parser) {
    Nfa* nfa = parser->nfa;
    char c = peek(parser);
    NfaState *start, *end;
    
    if (c == '(') {
        advance(parser);
        int group_id = parser->group_counter++;
        NfaState* group_start = nfa_state_new(nfa, NFA_SPLIT);
        start = parse_expr(parser);
        NfaState* group_end = nfa_state_new(nfa, NFA_SPLIT);
        if (!match_char(parser, ')')) {
            snprintf(nfa->error_msg, sizeof(nfa->error_msg), "missing closing parenthesis");
            return NULL;
        }
        group_start->out = start;
        group_start->group_id = group_id;
        NfaState* s = start;
        while (s && s->out) {
            if (s->type == NFA_SPLIT && s->out2) {
                if (!s->out2->out) {
                    s->out2->out = group_end;
                }
            }
            if (!s->out) break;
            s = s->out;
        }
        if (s) s->out = group_end;
        group_end->group_end = group_id;
        return group_start;
    } else if (c == '[') {
        advance(parser);
        unsigned char charset[32];
        charset_clear(charset);
        int negated = parse_char_class(parser, charset);
        if (negated < 0) {
            snprintf(nfa->error_msg, sizeof(nfa->error_msg), "invalid character class");
            return NULL;
        }
        start = nfa_state_new(nfa, negated ? NFA_NEG_CHARSET : NFA_CHARSET);
        memcpy(start->charset, charset, 32);
        end = nfa_state_new(nfa, NFA_SPLIT);
        start->out = end;
        return start;
    } else if (c == '.') {
        advance(parser);
        start = nfa_state_new(nfa, NFA_ANY);
        end = nfa_state_new(nfa, NFA_SPLIT);
        start->out = end;
        return start;
    } else if (c == '^') {
        advance(parser);
        return nfa_state_new(nfa, NFA_SPLIT);
    } else if (c == '$') {
        advance(parser);
        NfaState* s = nfa_state_new(nfa, NFA_SPLIT);
        return s;
    } else if (c == '\\' && parser->pattern[parser->pos + 1]) {
        advance(parser);
        char esc = advance(parser);
        start = nfa_state_new(nfa, NFA_CHAR);
        switch (esc) {
            case 'd':
                start->type = NFA_CHARSET;
                charset_clear(start->charset);
                charset_add_range(start->charset, '0', '9');
                break;
            case 'w':
                start->type = NFA_CHARSET;
                charset_clear(start->charset);
                charset_add_range(start->charset, 'a', 'z');
                charset_add_range(start->charset, 'A', 'Z');
                charset_add_range(start->charset, '0', '9');
                charset_set_bit(start->charset, '_');
                break;
            case 's':
                start->type = NFA_CHARSET;
                charset_clear(start->charset);
                charset_set_bit(start->charset, ' ');
                charset_set_bit(start->charset, '\t');
                charset_set_bit(start->charset, '\n');
                charset_set_bit(start->charset, '\r');
                charset_set_bit(start->charset, '\f');
                charset_set_bit(start->charset, '\v');
                break;
            case 'D':
                start->type = NFA_NEG_CHARSET;
                charset_clear(start->charset);
                charset_add_range(start->charset, '0', '9');
                break;
            case 'W':
                start->type = NFA_NEG_CHARSET;
                charset_clear(start->charset);
                charset_add_range(start->charset, 'a', 'z');
                charset_add_range(start->charset, 'A', 'Z');
                charset_add_range(start->charset, '0', '9');
                charset_set_bit(start->charset, '_');
                break;
            case 'S':
                start->type = NFA_NEG_CHARSET;
                charset_clear(start->charset);
                charset_set_bit(start->charset, ' ');
                charset_set_bit(start->charset, '\t');
                charset_set_bit(start->charset, '\n');
                charset_set_bit(start->charset, '\r');
                charset_set_bit(start->charset, '\f');
                charset_set_bit(start->charset, '\v');
                break;
            case 'n':
                start->c = '\n';
                break;
            case 't':
                start->c = '\t';
                break;
            case 'r':
                start->c = '\r';
                break;
            case '0':
                start->c = '\0';
                break;
            default:
                start->c = esc;
                break;
        }
        end = nfa_state_new(nfa, NFA_SPLIT);
        start->out = end;
        return start;
    } else if (c && c != '|' && c != ')' && c != '*' && c != '+' && c != '?' && c != ']') {
        advance(parser);
        start = nfa_state_new(nfa, NFA_CHAR);
        start->c = c;
        end = nfa_state_new(nfa, NFA_SPLIT);
        start->out = end;
        return start;
    }
    return nfa_state_new(nfa, NFA_SPLIT);
}

static NfaState* parse_quantifier(RegexParser* parser) {
    Nfa* nfa = parser->nfa;
    NfaState* atom = parse_atom(parser);
    if (!atom) return NULL;
    
    char c = peek(parser);
    if (c == '*' || c == '+' || c == '?') {
        advance(parser);
        NfaState* split = nfa_state_new(nfa, NFA_SPLIT);
        NfaState* end = nfa_state_new(nfa, NFA_SPLIT);
        
        NfaState* atom_end = atom;
        while (atom_end && atom_end->out) {
            atom_end = atom_end->out;
        }
        
        if (c == '*') {
            split->out = atom;
            split->out2 = end;
            if (atom_end) atom_end->out = split;
        } else if (c == '+') {
            split->out = atom;
            split->out2 = end;
            if (atom_end) atom_end->out = split;
            return atom;
        } else if (c == '?') {
            split->out = atom;
            split->out2 = end;
            if (atom_end) atom_end->out = end;
            return split;
        }
        return split;
    }
    return atom;
}

static NfaState* parse_concat(RegexParser* parser) {
    Nfa* nfa = parser->nfa;
    NfaState* first = NULL;
    NfaState* last = NULL;
    
    while (peek(parser) && peek(parser) != '|' && peek(parser) != ')') {
        NfaState* q = parse_quantifier(parser);
        if (!q) return NULL;
        if (!first) {
            first = q;
            last = q;
            while (last && last->out) {
                last = last->out;
            }
        } else {
            if (last) last->out = q;
            last = q;
            while (last && last->out) {
                last = last->out;
            }
        }
    }
    if (!first) {
        first = nfa_state_new(nfa, NFA_SPLIT);
    }
    return first;
}

static NfaState* parse_expr(RegexParser* parser) {
    Nfa* nfa = parser->nfa;
    NfaState* first = parse_concat(parser);
    if (!first) return NULL;
    
    if (match_char(parser, '|')) {
        NfaState* second = parse_expr(parser);
        if (!second) return NULL;
        NfaState* split = nfa_state_new(nfa, NFA_SPLIT);
        NfaState* end = nfa_state_new(nfa, NFA_SPLIT);
        split->out = first;
        split->out2 = second;
        
        NfaState* f = first;
        while (f && f->out) f = f->out;
        if (f) f->out = end;
        
        NfaState* s = second;
        while (s && s->out) s = s->out;
        if (s) s->out = end;
        
        return split;
    }
    return first;
}

static Nfa* nfa_compile(const char* pattern) {
    Nfa* nfa = (Nfa*)kmm_v4_malloc(sizeof(Nfa));
    if (!nfa) return NULL;
    memset(nfa, 0, sizeof(Nfa));
    nfa->states = (NfaState**)kmm_v4_malloc(sizeof(NfaState*) * MAX_STATES);
    if (!nfa->states) {
        kmm_v4_free(nfa);
        return NULL;
    }
    memset(nfa->states, 0, sizeof(NfaState*) * MAX_STATES);
    nfa->state_count = 0;
    nfa->group_count = 0;
    nfa->error_msg[0] = '\0';
    
    RegexParser parser;
    parser.pattern = pattern;
    parser.pos = 0;
    parser.nfa = nfa;
    parser.group_counter = 0;
    
    NfaState* expr_start = parse_expr(&parser);
    if (!expr_start) {
        return nfa;
    }
    
    NfaState* match_state = nfa_state_new(nfa, NFA_MATCH);
    NfaState* e = expr_start;
    while (e && e->out) e = e->out;
    if (e) e->out = match_state;
    
    nfa->start = expr_start;
    nfa->group_count = parser.group_counter;
    return nfa;
}

static void nfa_free(Nfa* nfa) {
    if (!nfa) return;
    int i;
    for (i = 0; i < nfa->state_count; i++) {
        if (nfa->states[i]) {
            kmm_v4_free(nfa->states[i]);
        }
    }
    if (nfa->states) kmm_v4_free(nfa->states);
    kmm_v4_free(nfa);
}

typedef struct {
    int found;
    size_t match_start;
    size_t match_end;
    GroupMatch groups[MAX_GROUPS];
    int group_count;
} MatchResult;

static int nfa_match_from(Nfa* nfa, const char* str, size_t start_pos, MatchResult* result) {
    size_t len = strlen(str);
    size_t pos;
    StateSet* current = build_start_set(nfa);
    if (!current) return 0;
    
    memset(result, 0, sizeof(MatchResult));
    result->match_start = start_pos;
    result->found = 0;
    
    if (has_match(current)) {
        result->found = 1;
        result->match_end = start_pos;
    }
    
    for (pos = start_pos; pos < len; pos++) {
        StateSet* next = step(current, str[pos], NULL, pos);
        state_set_free(current);
        current = next;
        if (!current) break;
        if (current->count == 0) break;
        if (has_match(current)) {
            result->found = 1;
            result->match_end = pos + 1;
        }
    }
    
    state_set_free(current);
    return result->found;
}

static void collect_groups(NfaState* state, GroupMatch* groups, size_t pos, int depth) {
    if (!state || depth > MAX_STATES) return;
    if (state->group_id >= 0) {
        if (groups[state->group_id].start == 0 && groups[state->group_id].end == 0) {
            groups[state->group_id].start = pos;
        }
    }
    if (state->group_end >= 0) {
        groups[state->group_end].end = pos;
    }
    if (state->type == NFA_SPLIT) {
        collect_groups(state->out, groups, pos, depth + 1);
        if (state->out2) {
            collect_groups(state->out2, groups, pos, depth + 1);
        }
    }
}

static int nfa_match_with_groups(Nfa* nfa, const char* str, size_t start_pos, MatchResult* result) {
    size_t len = strlen(str);
    size_t pos;
    GroupMatch groups[MAX_GROUPS];
    memset(groups, 0, sizeof(groups));
    
    StateSet* current = build_start_set(nfa);
    if (!current) return 0;
    
    memset(result, 0, sizeof(MatchResult));
    result->match_start = start_pos;
    result->group_count = nfa->group_count;
    result->found = 0;
    
    int i;
    for (i = 0; i < current->count; i++) {
        collect_groups(current->states[i], groups, start_pos, 0);
    }
    
    if (has_match(current)) {
        result->found = 1;
        result->match_end = start_pos;
        memcpy(result->groups, groups, sizeof(groups));
    }
    
    for (pos = start_pos; pos < len; pos++) {
        StateSet* next = step(current, str[pos], NULL, pos);
        state_set_free(current);
        current = next;
        if (!current) break;
        if (current->count == 0) break;
        
        for (i = 0; i < current->count; i++) {
            collect_groups(current->states[i], groups, pos + 1, 0);
        }
        
        if (has_match(current)) {
            result->found = 1;
            result->match_end = pos + 1;
            memcpy(result->groups, groups, sizeof(groups));
        }
    }
    
    state_set_free(current);
    return result->found;
}

Regex* regex_create(const char* pattern) {
    if (!pattern) return NULL;
    Regex* regex = (Regex*)kmm_v4_malloc(sizeof(Regex));
    if (!regex) return NULL;
    memset(regex, 0, sizeof(Regex));
    
    regex->pattern = pattern;
    Nfa* nfa = nfa_compile(pattern);
    if (!nfa) {
        kmm_v4_free(regex);
        return NULL;
    }
    regex->compiled = nfa;
    regex->valid = (nfa->error_msg[0] == '\0') ? 1 : 0;
    return regex;
}

void regex_destroy(Regex* regex) {
    if (!regex) return;
    if (regex->compiled) {
        nfa_free((Nfa*)regex->compiled);
        regex->compiled = NULL;
    }
    kmm_v4_free(regex);
}

bool_t regex_is_valid(const Regex* regex) {
    if (!regex) return 0;
    return regex->valid;
}

const char* regex_error(const Regex* regex) {
    if (!regex || !regex->compiled) return NULL;
    Nfa* nfa = (Nfa*)regex->compiled;
    if (nfa->error_msg[0] == '\0') return NULL;
    return nfa->error_msg;
}

bool_t regex_match(const Regex* regex, const char* str) {
    if (!regex || !regex->valid || !str) return 0;
    Nfa* nfa = (Nfa*)regex->compiled;
    if (!nfa) return 0;
    
    MatchResult result;
    if (nfa_match_from(nfa, str, 0, &result)) {
        return result.match_start == 0 && result.match_end == strlen(str);
    }
    return 0;
}

RegexMatch* regex_find(const Regex* regex, const char* str, size_t* count) {
    if (count) *count = 0;
    if (!regex || !regex->valid || !str) return NULL;
    Nfa* nfa = (Nfa*)regex->compiled;
    if (!nfa) return NULL;
    
    size_t len = strlen(str);
    size_t capacity = 16;
    size_t match_count = 0;
    RegexMatch* matches = (RegexMatch*)kmm_v4_malloc(sizeof(RegexMatch) * capacity);
    if (!matches) return NULL;
    
    size_t pos = 0;
    while (pos <= len) {
        MatchResult result;
        if (nfa_match_from(nfa, str, pos, &result) && result.match_end > result.match_start) {
            if (match_count >= capacity) {
                capacity *= 2;
                RegexMatch* new_matches = (RegexMatch*)kmm_v4_malloc(sizeof(RegexMatch) * capacity);
                if (!new_matches) {
                    size_t i;
                    for (i = 0; i < match_count; i++) {
                        if (matches[i].text) string_free(matches[i].text);
                    }
                    kmm_v4_free(matches);
                    return NULL;
                }
                memcpy(new_matches, matches, sizeof(RegexMatch) * match_count);
                kmm_v4_free(matches);
                matches = new_matches;
            }
            matches[match_count].start = result.match_start;
            matches[match_count].end = result.match_end;
            size_t match_len = result.match_end - result.match_start;
            matches[match_count].text = (char*)kmm_v4_malloc(match_len + 1);
            if (matches[match_count].text) {
                memcpy(matches[match_count].text, str + result.match_start, match_len);
                matches[match_count].text[match_len] = '\0';
            } else {
                matches[match_count].text = NULL;
            }
            match_count++;
            pos = result.match_end;
            if (result.match_start == result.match_end) {
                pos++;
            }
        } else {
            pos++;
        }
    }
    
    if (count) *count = match_count;
    return matches;
}

RegexMatch* regex_find_all(const Regex* regex, const char* str, size_t* count) {
    return regex_find(regex, str, count);
}

String regex_replace(const Regex* regex, const char* str, const char* replacement) {
    if (!regex || !regex->valid || !str || !replacement) return NULL;
    
    size_t match_count = 0;
    RegexMatch* matches = regex_find(regex, str, &match_count);
    if (!matches || match_count == 0) {
        if (matches) {
            size_t i;
            for (i = 0; i < match_count; i++) {
                if (matches[i].text) string_free(matches[i].text);
            }
            kmm_v4_free(matches);
        }
        return string_copy(str);
    }
    
    size_t str_len = strlen(str);
    size_t repl_len = strlen(replacement);
    size_t result_len = str_len;
    size_t i;
    for (i = 0; i < match_count; i++) {
        result_len = result_len - (matches[i].end - matches[i].start) + repl_len;
    }
    
    char* result = (char*)kmm_v4_malloc(result_len + 1);
    if (!result) {
        for (i = 0; i < match_count; i++) {
            if (matches[i].text) string_free(matches[i].text);
        }
        kmm_v4_free(matches);
        return NULL;
    }
    
    size_t pos = 0;
    size_t result_pos = 0;
    for (i = 0; i < match_count; i++) {
        if (matches[i].start > pos) {
            memcpy(result + result_pos, str + pos, matches[i].start - pos);
            result_pos += matches[i].start - pos;
        }
        memcpy(result + result_pos, replacement, repl_len);
        result_pos += repl_len;
        pos = matches[i].end;
    }
    if (pos < str_len) {
        memcpy(result + result_pos, str + pos, str_len - pos);
        result_pos += str_len - pos;
    }
    result[result_pos] = '\0';
    
    for (i = 0; i < match_count; i++) {
        if (matches[i].text) string_free(matches[i].text);
    }
    kmm_v4_free(matches);
    
    return result;
}

String* regex_split(const Regex* regex, const char* str, size_t* count) {
    if (count) *count = 0;
    if (!regex || !regex->valid || !str) return NULL;
    
    size_t match_count = 0;
    RegexMatch* matches = regex_find(regex, str, &match_count);
    
    size_t str_len = strlen(str);
    size_t part_count = 0;
    size_t capacity = match_count + 2;
    String* parts = (String*)kmm_v4_malloc(sizeof(String) * capacity);
    if (!parts) {
        if (matches) {
            size_t i;
            for (i = 0; i < match_count; i++) {
                if (matches[i].text) string_free(matches[i].text);
            }
            kmm_v4_free(matches);
        }
        return NULL;
    }
    
    size_t pos = 0;
    size_t i;
    for (i = 0; i < match_count; i++) {
        if (matches[i].start >= pos) {
            size_t part_len = matches[i].start - pos;
            parts[part_count] = (char*)kmm_v4_malloc(part_len + 1);
            if (parts[part_count]) {
                memcpy(parts[part_count], str + pos, part_len);
                parts[part_count][part_len] = '\0';
            } else {
                parts[part_count] = NULL;
            }
            part_count++;
            pos = matches[i].end;
        }
    }
    
    if (pos <= str_len) {
        size_t part_len = str_len - pos;
        parts[part_count] = (char*)kmm_v4_malloc(part_len + 1);
        if (parts[part_count]) {
            memcpy(parts[part_count], str + pos, part_len);
            parts[part_count][part_len] = '\0';
        } else {
            parts[part_count] = NULL;
        }
        part_count++;
    }
    
    if (matches) {
        for (i = 0; i < match_count; i++) {
            if (matches[i].text) string_free(matches[i].text);
        }
        kmm_v4_free(matches);
    }
    
    if (count) *count = part_count;
    return parts;
}

RegexMatch* regex_capture_groups(const Regex* regex, const char* str, size_t* count) {
    if (count) *count = 0;
    if (!regex || !regex->valid || !str) return NULL;
    Nfa* nfa = (Nfa*)regex->compiled;
    if (!nfa || nfa->group_count == 0) return NULL;
    
    MatchResult result;
    if (!nfa_match_with_groups(nfa, str, 0, &result)) {
        return NULL;
    }
    
    RegexMatch* groups = (RegexMatch*)kmm_v4_malloc(sizeof(RegexMatch) * nfa->group_count);
    if (!groups) return NULL;
    
    size_t len = strlen(str);
    int i;
    for (i = 0; i < nfa->group_count; i++) {
        groups[i].start = result.groups[i].start;
        groups[i].end = result.groups[i].end;
        if (result.groups[i].end > result.groups[i].start && result.groups[i].end <= len) {
            size_t glen = result.groups[i].end - result.groups[i].start;
            groups[i].text = (char*)kmm_v4_malloc(glen + 1);
            if (groups[i].text) {
                memcpy(groups[i].text, str + result.groups[i].start, glen);
                groups[i].text[glen] = '\0';
            } else {
                groups[i].text = NULL;
            }
        } else {
            groups[i].text = NULL;
        }
    }
    
    if (count) *count = nfa->group_count;
    return groups;
}

bool_t regex_match_simple(const char* pattern, const char* str) {
    Regex* regex = regex_create(pattern);
    if (!regex) return 0;
    bool_t result = regex_match(regex, str);
    regex_destroy(regex);
    return result;
}

String regex_replace_simple(const char* pattern, const char* str, const char* replacement) {
    Regex* regex = regex_create(pattern);
    if (!regex) return NULL;
    String result = regex_replace(regex, str, replacement);
    regex_destroy(regex);
    return result;
}

RegexMatch* regex_find_simple(const char* pattern, const char* str, size_t* count) {
    Regex* regex = regex_create(pattern);
    if (!regex) {
        if (count) *count = 0;
        return NULL;
    }
    RegexMatch* result = regex_find(regex, str, count);
    regex_destroy(regex);
    return result;
}
