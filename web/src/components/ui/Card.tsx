import type { ReactNode } from "react";

type Props = {
  title?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
};

export function Card({ title, actions, children }: Props) {
  return (
    <section className="card">
      {(title || actions) && (
        <header className="card-header">
          {title && <h2>{title}</h2>}
          {actions}
        </header>
      )}
      {children}
    </section>
  );
}
