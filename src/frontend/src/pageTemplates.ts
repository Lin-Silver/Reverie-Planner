const text = (text: string) => [{ type: 'text', text, styles: {} }]
const heading = (title: string) => ({ type: 'heading', props: { level: 2 }, content: text(title) })
const paragraph = (title: string) => ({ type: 'paragraph', content: text(title) })
const bullet = (title: string) => ({ type: 'bulletListItem', content: text(title) })
export const pageTemplates = [
 { id: 'blank', title: 'Blank page', icon: 'page', description: 'A clean space for your next idea.', blocks: [] },
 { id: 'notes', title: 'Meeting notes', icon: 'check', description: 'Decisions, discussion and clear next steps.', blocks: [heading('Meeting details'), paragraph('Date · Attendees · Purpose'), heading('Discussion'), bullet('What should we cover?'), heading('Decisions'), bullet('What did we agree on?'), heading('Next steps'), { type: 'checkListItem', content: text('Action · Owner · Due date') }] },
 { id: 'journal', title: 'Daily journal', icon: 'sun', description: 'Make room for a little reflection.', blocks: [heading('Today’s focus'), paragraph('What matters most today?'), heading('A few good things'), bullet('Something I’m grateful for'), heading('Notes to myself'), paragraph('A thought worth keeping…')] },
 { id: 'brief', title: 'Project brief', icon: 'folder', description: 'Turn a promising idea into a shared plan.', blocks: [heading('The idea'), paragraph('What are we building, and who is it for?'), heading('Success looks like'), bullet('A concrete outcome'), heading('Milestones'), { type: 'checkListItem', content: text('First milestone') }, heading('Open questions'), bullet('What do we need to learn?')] },
 { id: 'wiki', title: 'Knowledge base', icon: 'grid', description: 'Keep useful knowledge easy to find.', blocks: [heading('Overview'), paragraph('A short introduction to this topic.'), heading('Key ideas'), bullet('An idea worth remembering'), heading('Resources'), bullet('Add references and related pages'), heading('Questions'), paragraph('What should we explore next?')] },
]
