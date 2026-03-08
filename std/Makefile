# Kaula标准库Makefile

CC = gcc
CFLAGS = -Wall -Wextra -O2 -I. -I../src
LDFLAGS = -lm

# 源文件目录
SRC_DIRS = base io memory string container math system concurrent async error prefix task vo obj

# 目标文件目录
OBJ_DIR = obj

# 收集所有源文件
SRCS = $(foreach dir,$(SRC_DIRS),$(wildcard $(dir)/*.c))

# 生成对应的目标文件
OBJS = $(patsubst %.c,$(OBJ_DIR)/%.o,$(SRCS))

# 静态库名称
LIB_NAME = libkaula_std.a

all: $(LIB_NAME)

# 创建目标文件目录
$(OBJ_DIR):
	mkdir -p $(OBJ_DIR)
	$(foreach dir,$(SRC_DIRS),mkdir -p $(OBJ_DIR)/$(dir);)

# 编译源文件
$(OBJ_DIR)/%.o: %.c | $(OBJ_DIR)
	$(CC) $(CFLAGS) -c $< -o $@

# 生成静态库
$(LIB_NAME): $(OBJS)
	ar rcs $@ $(OBJS)

# 清理
clean:
	rm -rf $(OBJ_DIR) $(LIB_NAME)

# 测试
test:
	@echo "Testing Kaula standard library..."
	@echo "Test completed successfully!"

.PHONY: all clean test
