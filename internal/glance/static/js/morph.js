// morph updates oldEl's attributes and descendant tree to match newEl,
// patching nodes positionally in-place so that existing event listeners,
// scroll positions, and focused elements are preserved as much as possible.
export function morph(oldEl, newEl) {
    const newAttrs = newEl.attributes;
    for (let i = 0; i < newAttrs.length; i++) {
        const { name, value } = newAttrs[i];
        if (oldEl.getAttribute(name) !== value) oldEl.setAttribute(name, value);
    }
    const oldAttrs = Array.from(oldEl.attributes);
    for (const { name } of oldAttrs) {
        if (!newEl.hasAttribute(name)) oldEl.removeAttribute(name);
    }
    morphChildren(oldEl, newEl);
}

function morphChildren(oldParent, newParent) {
    const oldNodes = Array.from(oldParent.childNodes);
    const newNodes = Array.from(newParent.childNodes);

    for (let i = 0; i < newNodes.length; i++) {
        const newNode = newNodes[i];
        const oldNode = oldNodes[i];

        if (!oldNode) {
            oldParent.appendChild(newNode.cloneNode(true));
        } else if (oldNode.nodeType !== newNode.nodeType || oldNode.nodeName !== newNode.nodeName) {
            oldParent.replaceChild(newNode.cloneNode(true), oldNode);
        } else if (oldNode.nodeType === Node.TEXT_NODE || oldNode.nodeType === Node.COMMENT_NODE) {
            if (oldNode.nodeValue !== newNode.nodeValue) oldNode.nodeValue = newNode.nodeValue;
        } else if (oldNode.nodeType === Node.ELEMENT_NODE) {
            morph(oldNode, newNode);
        }
    }

    for (let i = newNodes.length; i < oldNodes.length; i++) {
        oldParent.removeChild(oldNodes[i]);
    }
}
