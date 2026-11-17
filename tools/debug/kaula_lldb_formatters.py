"""
Kaula LLDB Formatters

为 Kaula 语言类型提供 LLDB 类型摘要和合成器。

使用方法:
    # 在 LLDB 启动脚本中添加:
    command script import /path/to/kaula_lldb_formatters.py

    # 或在 LLDB 会话中:
    (lldb) command script import /path/to/kaula_lldb_formatters.py
"""

import lldb


def kaula_string_summary(valobj, internal_dict):
    """String 类型摘要: 显示字符串内容"""
    try:
        len_val = valobj.GetChildMemberWithName("len").GetValueAsUnsigned()
        ptr_val = valobj.GetChildMemberWithName("ptr")
        if len_val == 0:
            return '""'
        # 读取内存
        error = lldb.SBError()
        process = valobj.GetTarget().GetProcess()
        data = process.ReadMemory(ptr_val.GetValueAsUnsigned(), len_val, error)
        if error.Fail():
            return f"(read error: {error})"
        return f'"{data.decode("utf-8", errors="replace")}"'
    except Exception as e:
        return f"(invalid String: {e})"


def kaula_option_summary(valobj, internal_dict):
    """Option<T> 类型摘要"""
    try:
        tag = valobj.GetChildMemberWithName("tag").GetValueAsSigned()
        if tag == 0:
            return "None"
        value = valobj.GetChildMemberWithName("value")
        return f"Some({value})"
    except Exception:
        return "(invalid Option)"


def kaula_result_summary(valobj, internal_dict):
    """Result<T,E> 类型摘要"""
    try:
        tag = valobj.GetChildMemberWithName("tag").GetValueAsSigned()
        if tag == 0:
            ok = valobj.GetChildMemberWithName("ok")
            return f"Ok({ok})"
        else:
            err = valobj.GetChildMemberWithName("err")
            return f"Err({err})"
    except Exception:
        return "(invalid Result)"


def kaula_error_summary(valobj, internal_dict):
    """Error 类型摘要"""
    try:
        code = valobj.GetChildMemberWithName("code").GetValueAsSigned()
        msg_obj = valobj.GetChildMemberWithName("msg")
        msg_ptr = msg_obj.GetChildMemberWithName("ptr")
        msg_len = msg_obj.GetChildMemberWithName("len").GetValueAsUnsigned()
        if msg_len > 0:
            process = valobj.GetTarget().GetProcess()
            error = lldb.SBError()
            data = process.ReadMemory(msg_ptr.GetValueAsUnsigned(), msg_len, error)
            if error.Fail():
                return f"Error(code={code}, msg=<read error>)"
            msg = data.decode("utf-8", errors="replace")
            return f'Error(code={code}, msg="{msg}")'
        return f"Error(code={code})"
    except Exception:
        return "(invalid Error)"


def kaula_slice_summary(valobj, internal_dict):
    """Slice 类型摘要"""
    try:
        ptr = valobj.GetChildMemberWithName("ptr")
        length = valobj.GetChildMemberWithName("len").GetValueAsUnsigned()
        return f"[{length}] (ptr={ptr.GetValueAsUnsigned():#x})"
    except Exception:
        return "(invalid Slice)"


class KaulaStringSynthProvider:
    """String 类型合成器: 展开 len 和 ptr 字段"""

    def __init__(self, valobj, internal_dict):
        self.valobj = valobj

    def num_children(self):
        return 2

    def get_child_index(self, name):
        if name == "len":
            return 0
        elif name == "ptr":
            return 1
        return -1

    def get_child_at_index(self, index):
        if index == 0:
            return self.valobj.GetChildMemberWithName("len")
        elif index == 1:
            return self.valobj.GetChildMemberWithName("ptr")
        return None


class KaulaOptionSynthProvider:
    """Option<T> 合成器: 展开 tag 和 value"""

    def __init__(self, valobj, internal_dict):
        self.valobj = valobj

    def num_children(self):
        return 2

    def get_child_index(self, name):
        if name == "tag":
            return 0
        elif name == "value":
            return 1
        return -1

    def get_child_at_index(self, index):
        if index == 0:
            return self.valobj.GetChildMemberWithName("tag")
        elif index == 1:
            return self.valobj.GetChildMemberWithName("value")
        return None


def __lldb_init_module(debugger, internal_dict):
    """注册所有 Kaula 类型的 LLDB 格式化器"""
    # 注册摘要
    debugger.HandleCommand(
        'type summary add -F kaula_lldb_formatters.kaula_string_summary String',
    )
    debugger.HandleCommand(
        'type summary add -F kaula_lldb_formatters.kaula_error_summary Error',
    )
    debugger.HandleCommand(
        'type summary add -F kaula_lldb_formatters.kaula_slice_summary Slice',
    )
    debugger.HandleCommand(
        'type summary add --regex -F kaula_lldb_formatters.kaula_option_summary "^Option<.+>$"',
    )
    debugger.HandleCommand(
        'type summary add --regex -F kaula_lldb_formatters.kaula_result_summary "^Result<.+>$"',
    )

    # 注册合成器
    debugger.HandleCommand(
        'type synthetic add -l kaula_lldb_formatters.KaulaStringSynthProvider String',
    )
    debugger.HandleCommand(
        'type synthetic add -l kaula_lldb_formatters.KaulaOptionSynthProvider --regex "^Option<.+>$"',
    )

    print("Kaula LLDB formatters loaded.")
