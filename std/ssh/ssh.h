#pragma once
#include "../base/types.h"
#include "../memory/memory.h"
#include "../string/string.h"

typedef struct SSHSession {
    String host;
    i64 port;
    String username;
    String password;
    bool_t connected;
} SSHSession;

typedef struct SSHChannel {
    SSHSession* session;
    String channel_type;
    bool_t open;
} SSHChannel;

SSHSession* ssh_session_create(const char* host, i64 port, const char* username, const char* password);
void ssh_session_destroy(SSHSession* session);

bool_t ssh_connect(SSHSession* session);
bool_t ssh_disconnect(SSHSession* session);

bool_t ssh_authenticate_password(SSHSession* session, const char* password);
bool_t ssh_authenticate_public_key(SSHSession* session, const char* private_key_path);

bool_t ssh_is_connected(const SSHSession* session);
String ssh_get_server_version(SSHSession* session);
String ssh_get_host_key(SSHSession* session);

SSHChannel* ssh_channel_open_session(SSHSession* session);
SSHChannel* ssh_channel_open_sftp(SSHSession* session);
void ssh_channel_close(SSHChannel* channel);
void ssh_channel_destroy(SSHChannel* channel);

ssize_t ssh_channel_read(SSHChannel* channel, u8* buffer, size_t size);
ssize_t ssh_channel_write(SSHChannel* channel, const u8* buffer, size_t size);

bool_t ssh_channel_request_exec(SSHChannel* channel, const char* command);
bool_t ssh_channel_request_shell(SSHChannel* channel);

bool_t ssh_sftp_stat(SSHChannel* channel, const char* path, void* stat_info);
bool_t ssh_sftp_list_dir(SSHChannel* channel, const char* path);
bool_t ssh_sftp_get(SSHChannel* channel, const char* remote_path, const char* local_path);
bool_t ssh_sftp_put(SSHChannel* channel, const char* local_path, const char* remote_path);
bool_t ssh_sftp_mkdir(SSHChannel* channel, const char* path);
bool_t ssh_sftp_rmdir(SSHChannel* channel, const char* path);
bool_t ssh_sftp_remove(SSHChannel* channel, const char* path);

bool_t ssh_port_forward(SSHSession* session, const char* bind_addr, i64 bind_port,
                        const char* dest_addr, i64 dest_port);