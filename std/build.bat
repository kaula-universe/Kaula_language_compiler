@echo off

rem 使用Clang编译器编译所有模块并生成静态库

echo 开始编译...

rem 编译src目录中的高性能组件
clang -Wall -Wextra -O2 -I. -I../src -c ../src/allocator.c -o allocator.o
clang -Wall -Wextra -O2 -I. -I../src -c ../src/vo.c -o vo_system.o
clang -Wall -Wextra -O2 -I. -I../src -c ../src/queue.c -o queue.o
clang -Wall -Wextra -O2 -I. -I../src -c ../src/spend_call.c -o spend_call.o
clang -Wall -Wextra -O2 -I. -I../src -c ../src/time.c -o time.o

rem 编译std库模块
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c base/types.c -o base_types.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c io/io.c -o io.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c memory/memory.c -o memory.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c memory/memory_pool.c -o memory_pool.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c string/string.c -o string.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c container/container.c -o container.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c math/math.c -o math.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c system/system.c -o system.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c concurrent/concurrent.c -o concurrent.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c async/async.c -o async.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c error/error.c -o error.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c prefix/prefix.c -o prefix.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c task/task.c -o task.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c vo/vo.c -o vo.o

rem 编译对象系统模块
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c obj/object.c -o obj_object.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c obj/int_object.c -o obj_int_object.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c obj/float_object.c -o obj_float_object.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c obj/bool_object.c -o obj_bool_object.o
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c obj/string_object.c -o obj_string_object.o

rem 生成静态库
echo 生成静态库...
llvm-ar rcs libkaula_std.a allocator.o vo_system.o queue.o spend_call.o time.o base_types.o io.o memory.o memory_pool.o string.o container.o math.o system.o concurrent.o async.o error.o prefix.o task.o vo.o obj_object.o obj_int_object.o obj_float_object.o obj_bool_object.o obj_string_object.o

rem 编译异步测试程序
clang -Wall -Wextra -O2 -I. -I../src -D_WIN32 -c test_async.c -o test_async.o
clang -Wall -Wextra -O2 -o test_async.exe test_async.o libkaula_std.a -ladvapi32 -luser32 -lgdi32 -lws2_32

echo 编译完成！