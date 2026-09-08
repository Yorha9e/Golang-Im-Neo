import React from 'react';
import { Crown, ShieldCheck } from 'lucide-react';

interface RoleBadgeProps {
  role?: string;
  username?: string;
  size?: 'sm' | 'md';
}

export const RoleBadge: React.FC<RoleBadgeProps> = ({
  role = 'user',
  username,
  size = 'sm',
}) => {
  const isSuperAdmin = role === 'superadmin' || username === 'superadmin';
  const isAdmin = role === 'admin';

  if (!isSuperAdmin && !isAdmin) return null;

  if (isSuperAdmin) {
    return (
      <span
        className={`inline-flex items-center gap-1 font-semibold rounded-md shadow-warm-sm select-none ${
          size === 'sm'
            ? 'text-[10px] px-1.5 py-0.2 bg-gradient-to-r from-amber-500 to-[#8B7355] text-white'
            : 'text-xs px-2 py-0.5 bg-gradient-to-r from-amber-500 to-[#8B7355] text-white'
        }`}
      >
        <Crown size={size === 'sm' ? 11 : 13} className="text-amber-100" />
        <span>SuperAdmin</span>
      </span>
    );
  }

  if (isAdmin) {
    return (
      <span
        className={`inline-flex items-center gap-1 font-semibold rounded-md shadow-warm-sm select-none ${
          size === 'sm'
            ? 'text-[10px] px-1.5 py-0.2 bg-gradient-to-r from-blue-600 to-indigo-600 text-white'
            : 'text-xs px-2 py-0.5 bg-gradient-to-r from-blue-600 to-indigo-600 text-white'
        }`}
      >
        <ShieldCheck size={size === 'sm' ? 11 : 13} className="text-blue-100" />
        <span>Admin</span>
      </span>
    );
  }

  return null;
};
