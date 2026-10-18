import type { ButtonHTMLAttributes } from "react";

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "default" | "ghost" | "primary";
};

export function Button({ variant = "default", className = "", ...rest }: Props) {
  return <button type="button" className={`btn btn-${variant} ${className}`} {...rest} />;
}
