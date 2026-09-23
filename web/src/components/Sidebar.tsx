import { useEffect, useState } from 'react';
import type { AdminUser } from '../types';

type SidebarProps = {
  user: AdminUser;
  onLogout: () => void;
};

const links = [
  { hash: '#dashboard', label: 'Dashboard' },
  { hash: '#bot-data', label: 'Bot 数据' },
  { hash: '#system', label: '系统配置' },
];

const currentHash = () => window.location.hash || '#dashboard';

export const Sidebar = ({ user, onLogout }: SidebarProps) => {
  const [hash, setHash] = useState(currentHash);

  useEffect(() => {
    const onChange = () => setHash(currentHash());
    window.addEventListener('hashchange', onChange);
    return () => window.removeEventListener('hashchange', onChange);
  }, []);

  return (
    <aside className="sidebar">
      <div>
        <p className="eyebrow">Gokohime</p>
        <h1>Admin</h1>
      </div>
      <nav>
        {links.map((link) => (
          <a
            key={link.hash}
            className={hash === link.hash ? 'active' : undefined}
            aria-current={hash === link.hash ? 'true' : undefined}
            href={link.hash}
          >
            {link.label}
          </a>
        ))}
      </nav>
      <button className="button ghost" onClick={onLogout} type="button">
        退出 {user.username}
      </button>
    </aside>
  );
};
