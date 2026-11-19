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
}: {
  content: ReactNode;
  count: number;
}) {
  const commands = count === 1 ? collect(content) : [];
  const common = commands.filter((a) =>
    /^(详情|查看详情|编辑|编辑IP|编辑 IP|启用|停用|启动|停止)$/.test(a.label),
  );
  const other = commands.filter((a) => !common.includes(a));
  return (
    <div className="actions selection-commands">
      {count !== 1 ? (
        <>
          <Button disabled>详情</Button>
          <Button disabled>编辑</Button>
        </>
      ) : (
        common.map((item, i) => (
          <Button
            key={i}
            disabled={item.disabled}
            onClick={() => item.onClick?.()}
            title={item.title}
          >
            {item.label.replace("查看详情", "详情")}
          </Button>
        ))
      )}
      <ActionMenu
        label="操作"
        items={
          other.length
            ? other
            : [
                {
                  label: count > 1 ? "请选择单个对象操作" : "请先选择对象",
                  disabled: true,
                },
              ]
        }
      />
    </div>
  );
}
