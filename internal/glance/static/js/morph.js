// morph updates oldEl's attributes and descendant tree to match newEl,
// patching nodes positionally in-place so that existing event listeners,
// scroll positions, and focused elements are preserved as much as possible.
//
// When prevEl is given — the server HTML that oldEl was last rendered or
// patched from — only what changed between prevEl and newEl is applied.
// Whatever scripts did to the page (text filled into an empty element,
// classes added, nodes inserted) survives as long as the server did not
// change that part, without the scripts having to re-run.
export function morph(oldEl, newEl, prevEl) {
    if (prevEl) {
        morphAttributesFrom(oldEl, prevEl, newEl);
        morphChildrenFrom(oldEl, prevEl, newEl);
        return;
    }

    const prevClasses = new Set(oldEl.classList);

    const newAttrs = newEl.attributes;
    for (let i = 0; i < newAttrs.length; i++) {
        const { name, value } = newAttrs[i];
        if (oldEl.getAttribute(name) !== value) oldEl.setAttribute(name, value);
    }
    const oldAttrs = Array.from(oldEl.attributes);
    for (const { name } of oldAttrs) {
        if (!newEl.hasAttribute(name)) oldEl.removeAttribute(name);
    }

    for (const cls of prevClasses) {
        if (!newEl.classList.contains(cls)) oldEl.classList.add(cls);
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

// Applies the attribute changes between prev and next to el; classes one by one,
// so that classes added by scripts stay and classes the server dropped go.
function morphAttributesFrom(el, prev, next) {
    for (const { name, value } of Array.from(next.attributes)) {
        const prevValue = prev.getAttribute(name);
        if (prevValue === value) continue;
        if (name === 'class') {
            for (const cls of prev.classList) {
                if (!next.classList.contains(cls)) el.classList.remove(cls);
            }
            for (const cls of next.classList) el.classList.add(cls);
        } else {
            el.setAttribute(name, value);
        }
    }
    for (const { name } of Array.from(prev.attributes)) {
        if (next.hasAttribute(name)) continue;
        if (name === 'class') {
            for (const cls of prev.classList) el.classList.remove(cls);
        } else {
            el.removeAttribute(name);
        }
    }
}

function sameKind(a, b) {
    return a.nodeType === b.nodeType && a.nodeName === b.nodeName;
}

function morphChildrenFrom(parent, prevParent, nextParent) {
    const nodes = parent.childNodes;
    const prevNodes = prevParent.childNodes;
    const nextNodes = nextParent.childNodes;

    // The page's children line up with the old server children and the server kept the
    // shape: patch pair by pair, skipping every subtree the server sent unchanged.
    let aligned = nodes.length === prevNodes.length && prevNodes.length === nextNodes.length;
    for (let i = 0; aligned && i < nextNodes.length; i++) {
        aligned = sameKind(nodes[i], prevNodes[i]) && sameKind(prevNodes[i], nextNodes[i]);
    }

    if (!aligned) {
        // the server changed the shape here, or a script did: the server wins for this subtree
        morphChildren(parent, nextParent);
        return;
    }

    for (let i = 0; i < nextNodes.length; i++) {
        const node = nodes[i];
        const prevNode = prevNodes[i];
        const nextNode = nextNodes[i];

        if (prevNode.isEqualNode(nextNode)) continue;

        if (node.nodeType === Node.ELEMENT_NODE) {
            morph(node, nextNode, prevNode);
        } else if (node.nodeValue !== nextNode.nodeValue) {
            node.nodeValue = nextNode.nodeValue;
        }
    }
}
