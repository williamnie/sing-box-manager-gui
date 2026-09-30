import { Textarea } from '@nextui-org/react';
import { parseList } from '../utils/lists';

// 保留编辑中的空行和分隔符，避免每次输入后规范化导致光标跳动。
export default function ListInput({ label, values, onChange, description, placeholder }: {
  label: string; values: string[]; onChange: (values: string[]) => void; description?: string; placeholder?: string;
}) {
  return <Textarea label={label} defaultValue={values.join('\n')} onValueChange={(text) => onChange(parseList(text))} description={description} placeholder={placeholder} minRows={1} maxRows={5} />;
}
