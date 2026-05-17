#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "../src/kaula.h"
#include "../std/web/web.h"
#include "../std/json/json.h"
#include "../std/time/time.h"
#include "../std/memory/memory.h"
#include "../std/io/io.h"
#include "../std/string/string.h"

void send_chat(HttpClient* client, char* json_tpl) {
    KMM_V4_SCOPE_START {
        char* ct = "application/json";
        char* url = "https://open.bigmodel.cn/api/paas/v4/chat/completions";
        HttpResponse* resp = http_client_post(client, url, json_tpl, ct);
        char* rb = http_response_to_string(resp);
        JsonValue* root = json_parse(rb);
        JsonValue* choices = json_object_get(root, "choices");
        char* glm_resp = "";
        if (choices != NULL) {
            JsonValue* first = json_array_get(choices, 0);
            JsonValue* msg = json_object_get(first, "message");
            JsonValue* cv = json_object_get(msg, "content");
            glm_resp = json_serialize(cv);
            json_destroy(cv);
            json_destroy(msg);
            json_destroy(first);
        }
        json_destroy(root);
        json_destroy(choices);
        println_multi(2, 2, "GLM: ", 2, glm_resp);
        fast_free(rb);
        http_response_destroy(resp);
    } KMM_V4_SCOPE_END;
}

int main() {
    KMM_V4_SCOPE_START {
        println("======================================");
        println("  Kaula GLM-4.5-Air Chat CLI");
        println("======================================");
        HttpClient* client = http_client_create();
        http_client_set_timeout(client, 30000);
        JsonValue* body = json_create_object();
        JsonValue* mv = json_create_string("glm-4.5-air");
        json_object_set(body, "model", mv);
        JsonValue* msgs = json_create_array();
        JsonValue* msg = json_create_object();
        JsonValue* role = json_create_string("user");
        json_object_set(msg, "role", role);
        json_array_append(msgs, msg);
        json_object_set(body, "messages", msgs);
        char* json_tpl = json_serialize(body);
        json_destroy(body);
        json_destroy(mv);
        json_destroy(msgs);
        json_destroy(msg);
        json_destroy(role);
        int64_t running = 1;
        while (running == 1) {
            print("You: ");
            char* input = read_line();
            int64_t cmp_q = string_compare(input, "quit");
            int64_t cmp_e = string_compare(input, "exit");
            int64_t do_quit = 0;
            if (cmp_q == 0) {
                do_quit = 1;
            }
            if (cmp_e == 0) {
                do_quit = 1;
            }
            if (do_quit == 1) {
                println("Goodbye!");
                running = 0;
            }
            int64_t cmp_h = string_compare(input, "help");
            if (cmp_h == 0) {
                println("Commands: help, quit, <text>");
            }
            if (do_quit == 0) {
                if (cmp_h != 0) {
                    println("Thinking...");
                    send_chat(client, json_tpl);
                }
            }
            string_free(input);
        }
        string_free(json_tpl);
        http_client_destroy(client);
    } KMM_V4_SCOPE_END;
    return 0;
}

