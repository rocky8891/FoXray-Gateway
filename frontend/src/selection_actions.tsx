import { Children, isValidElement, type ReactNode } from "react";
import { ActionMenu, type MenuAction } from "./action_menu";
import { Button } from "./ui";

function collect(node: ReactNode): MenuAction[] {
  const result: MenuAction[] = [];
  Children.forEach(node, (child) => {
    if (!isValidElement(child)) return;
    const props = child.props as {
      children?: ReactNode;
      items?: MenuAction[];
      primary?: MenuAction;
      onClick?: () => void;
      disabled?: boolean;
      title?: string;
    };
    if (props.items)
      result.push(...(props.primary ? [props.primary] : []), ...props.items);
    else if (props.onClick) {
      const text = (value: ReactNode): string =>
        Children.toArray(value)
          .map((x) =>
            typeof x === "string" || typeof x === "number"
              ? String(x)
              : isValidElement(x)
                ? text((x.props as { children?: ReactNode }).children)
                : "",
          )
          .join("")
          .trim();
      result.push({
        label: text(props.children),
        onClick: props.onClick,
        disabled: props.disabled,
        title: props.title,
      });
    } else if (props.children) result.push(...collect(props.children));
  });
  return result;
}
export function SelectionActions({
  content,
  count,
  bulk,
}: {
  content: ReactNode;
  count: number;
  bulk?: MenuAction[];
}) {
  const commands = count === 1 ? collect(content) : [],
    available = bulk || commands;
  const enable = available.find((a) => /^(启用|启动)$/.test(a.label)),
    disable = available.find((a) => /^(停用|停止)$/.test(a.label));
  const other = (bulk ?? (count === 1 ? commands : available)).filter(
    (a) => !/^(启用|停用|启动|停止)$/.test(a.label),
  );
  return (
    <div className="actions selection-commands">
      <Button
        disabled={!count || !enable || enable.disabled}
        onClick={() => enable?.onClick?.()}
        title={enable?.title || (!count ? "请先选择对象" : undefined)}
      >
        启用
      </Button>
      <Button
        disabled={!count || !disable || disable.disabled}
        onClick={() => disable?.onClick?.()}
        title={disable?.title || (!count ? "请先选择对象" : undefined)}
      >
        停用
      </Button>
      <ActionMenu
        label="所选对象"
        triggerLabel="批量操作"
        items={
          other.length
            ? other
            : [
                {
                  label: "无可用批量操作",
                  disabled: true,
                  title: count ? "请使用行操作" : "请先选择对象",
                },
              ]
        }
      />
    </div>
  );
}
